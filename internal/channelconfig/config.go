// Package channelconfig stores provider parameters in the owning service. Only
// that service can decrypt credentials; administrative reads return presence flags.
package channelconfig

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"

	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type Field struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Secret   bool     `json:"secret"`
	Help     string   `json:"help,omitempty"`
	Options  []string `json:"options,omitempty"`
}
type Schema struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Fields []Field `json:"fields"`
}
type Document struct {
	Values                map[string]string `json:"values"`
	CredentialsCiphertext string            `json:"credentials_ciphertext"`
}
type Profile struct {
	Values  map[string]string
	Version int64
	Source  string
}

// A dedicated key is preferred. An existing private RPC credential can provide
// the stable bootstrap key without putting a second copy of it in the database.
func Key() ([]byte, error) {
	if raw := os.Getenv("CHANNEL_CONFIG_ENCRYPTION_KEY"); raw != "" {
		decoded, e := hex.DecodeString(raw)
		if e != nil || len(decoded) != 32 {
			decoded, e = base64.StdEncoding.DecodeString(raw)
		}
		if e != nil || len(decoded) != 32 {
			return nil, status.Error(codes.FailedPrecondition, "渠道加密密钥必须是32字节的hex或base64")
		}
		return decoded, nil
	}
	if token := os.Getenv("INTERNAL_RPC_TOKEN"); len(token) >= 16 {
		v := sha256.Sum256([]byte("lumo.channel-credentials.v1\x00" + token))
		return v[:], nil
	}
	if os.Getenv("DEMO_MODE") == "true" || os.Getenv("DEMO_MODE") == "1" {
		v := sha256.Sum256([]byte("lumo.isolated-demo.channel-credentials.v1"))
		return v[:], nil
	}
	return nil, status.Error(codes.FailedPrecondition, "请配置服务端渠道加密密钥")
}
func CryptReady() bool { _, e := Key(); return e == nil }
func cryptor() (cipher.AEAD, error) {
	key, e := Key()
	if e != nil {
		return nil, e
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func decrypt(id, encoded string) (map[string]string, error) {
	values := map[string]string{}
	if encoded == "" {
		return values, nil
	}
	box, e := cryptor()
	if e != nil {
		return nil, e
	}
	raw, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil || len(raw) < box.NonceSize() {
		return nil, status.Error(codes.FailedPrecondition, "渠道凭据无法解密，请检查服务器密钥")
	}
	plain, e := box.Open(nil, raw[:box.NonceSize()], raw[box.NonceSize():], []byte(id))
	if e != nil {
		return nil, status.Error(codes.FailedPrecondition, "渠道凭据无法解密，请检查服务器密钥")
	}
	if e = json.Unmarshal(plain, &values); e != nil {
		return nil, status.Error(codes.Internal, "渠道凭据格式无效")
	}
	return values, nil
}
func seal(id string, values map[string]string) (string, error) {
	box, e := cryptor()
	if e != nil {
		return "", e
	}
	nonce := make([]byte, box.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	plain, e := json.Marshal(values)
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(box.Seal(nonce, nonce, plain, []byte(id))), nil
}
func Read(ctx context.Context, db *gorm.DB, id string, defaults map[string]string) (Profile, error) {
	ctx, sp := platform.Start(ctx, "channelconfig.repository.Read")
	defer sp.End()
	doc, version, e := adminconfig.Read(ctx, db, id, Document{})
	if e != nil {
		return Profile{}, e
	}
	p := Profile{Version: version, Values: map[string]string{}, Source: "database"}
	if version == 0 {
		for k, v := range defaults {
			p.Values[k] = v
		}
		p.Source = "environment"
		return p, nil
	}
	for k, v := range doc.Values {
		p.Values[k] = v
	}
	secrets, e := decrypt(id, doc.CredentialsCiphertext)
	if e != nil {
		return Profile{}, e
	}
	for k, v := range secrets {
		p.Values[k] = v
	}
	return p, nil
}
func Public(schema Schema, p Profile) platform.Result {
	values := map[string]string{}
	configured := map[string]bool{}
	missing := []string{}
	for _, f := range schema.Fields {
		v := p.Values[f.Key]
		if f.Secret {
			configured[f.Key] = v != ""
		} else {
			values[f.Key] = v
		}
		if f.Required && strings.TrimSpace(v) == "" {
			missing = append(missing, f.Label)
		}
	}
	return platform.Result{"id": schema.ID, "name": schema.Name, "fields": schema.Fields, "values": values, "secret_configured": configured, "version": p.Version, "source": p.Source, "missing": missing, "configured": len(missing) == 0, "secure_storage_ready": CryptReady()}
}
func Merge(schema Schema, current Profile, c platform.Request) (Profile, error) {
	var input struct {
		Values map[string]string `json:"values"`
		Clear  []string          `json:"clear_secrets"`
	}
	if e := platform.Decode(c.Data, &input); e != nil {
		return Profile{}, e
	}
	if input.Values == nil {
		return Profile{}, status.Error(codes.InvalidArgument, "请填写渠道参数")
	}
	allowed := map[string]Field{}
	for _, f := range schema.Fields {
		allowed[f.Key] = f
	}
	next := Profile{Values: map[string]string{}, Version: current.Version, Source: "database"}
	for k, v := range current.Values {
		next.Values[k] = v
	}
	for k, v := range input.Values {
		f, ok := allowed[k]
		if !ok {
			return Profile{}, status.Error(codes.InvalidArgument, "包含不支持的渠道参数")
		}
		if len(v) > 32768 {
			return Profile{}, status.Error(codes.InvalidArgument, "渠道参数过长")
		}
		if f.Type != "textarea" {
			v = strings.TrimSpace(v)
			if strings.ContainsAny(v, "\r\n") {
				return Profile{}, status.Error(codes.InvalidArgument, "渠道参数不能包含换行")
			}
		}
		if f.Secret && v == "" {
			continue
		}
		if len(f.Options) > 0 && v != "" {
			found := false
			for _, option := range f.Options {
				found = found || v == option
			}
			if !found {
				return Profile{}, status.Error(codes.InvalidArgument, "渠道参数选项无效")
			}
		}
		next.Values[k] = v
	}
	for _, k := range input.Clear {
		f, ok := allowed[k]
		if !ok || !f.Secret {
			return Profile{}, status.Error(codes.InvalidArgument, "只能清除指定的密钥字段")
		}
		next.Values[k] = ""
	}
	return next, nil
}
func Save(ctx context.Context, db *gorm.DB, id string, schema Schema, p Profile, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "channelconfig.repository.Save")
	defer sp.End()
	public, secrets := map[string]string{}, map[string]string{}
	for _, f := range schema.Fields {
		if f.Secret {
			secrets[f.Key] = p.Values[f.Key]
		} else {
			public[f.Key] = p.Values[f.Key]
		}
	}
	encoded, e := seal(id, secrets)
	if e != nil {
		return nil, e
	}
	r, e := adminconfig.Save(ctx, db, id, Document{Values: public, CredentialsCiphertext: encoded}, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
	if e != nil {
		return nil, e
	}
	p.Version = platform.Int(r, "version")
	return Public(schema, p), nil
}
