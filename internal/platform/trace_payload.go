package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	tracePayloadMaxBytes  = 8 << 10
	tracePayloadMaxDepth  = 8
	tracePayloadMaxNodes  = 256
	tracePayloadMaxItems  = 10
	tracePayloadMaxFields = 64
	tracePayloadMaxString = 512
	tracePayloadMaxJSON   = 4 << 20
	traceRedacted         = "[REDACTED]"
	traceTruncated        = "[TRUNCATED]"
)

// TraceRequest records a bounded, detached preview. It never modifies the
// business value, serializes request headers, or invokes custom JSON marshalers.
func TraceRequest(ctx context.Context, value any) {
	tracePayload(trace.SpanFromContext(ctx), "lumo.request", value)
}

// TraceResponse uses status codes for errors. Error text can contain user input,
// database values or credentials and must not bypass the payload redactor.
func TraceResponse(ctx context.Context, value any, err error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	code := codes.OK
	if err != nil {
		code = traceErrorCode(err)
		span.SetStatus(otelcodes.Error, code.String())
		if value == nil {
			value = map[string]any{"error": map[string]any{"code": code.String()}}
		}
	}
	span.SetAttributes(attribute.String("lumo.error.code", code.String()))
	tracePayload(span, "lumo.response", value)
}

func traceErrorCode(err error) codes.Code {
	switch {
	case errors.Is(err, context.Canceled):
		return codes.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return codes.DeadlineExceeded
	}
	code := status.Code(err)
	if code == codes.Unknown {
		return codes.Internal
	}
	return code
}

func tracePayload(span trace.Span, key string, value any) {
	if !span.IsRecording() {
		return
	}
	preview := &payloadPreview{}
	clean := preview.value(reflect.ValueOf(value), "", 0)
	preview.redactIdentityIDs(clean)
	encoded, err := json.Marshal(clean)
	if err != nil {
		encoded = []byte(`"[UNAVAILABLE]"`)
		preview.truncated = true
	}
	if len(encoded) > tracePayloadMaxBytes {
		preview.truncated = true
		// The fallback remains valid JSON and contains only already-sanitized
		// text. Binary search accounts for JSON escaping and UTF-8 boundaries.
		low, high := 0, min(len(encoded), tracePayloadMaxBytes)
		for low < high {
			middle := (low + high + 1) / 2
			candidate, _ := json.Marshal(map[string]any{"_truncated": true, "preview": validPrefix(string(encoded), middle)})
			if len(candidate) <= tracePayloadMaxBytes {
				low = middle
			} else {
				high = middle - 1
			}
		}
		encoded, _ = json.Marshal(map[string]any{"_truncated": true, "preview": validPrefix(string(encoded), low)})
	}
	span.SetAttributes(attribute.String(key, string(encoded)), attribute.Bool(key+".truncated", preview.truncated), attribute.Bool(key+".redacted", preview.redacted))
}

type payloadPreview struct {
	nodes               int
	truncated, redacted bool
	identities          map[string]struct{}
}

func identityTraceKey(key string) bool {
	switch normalizedTraceKey(key) {
	case "userid", "uid", "username", "ownerid", "memberid", "accountid", "customerid":
		return true
	default:
		return false
	}
}

// Some account/profile aggregates reuse a principal identifier as their plain
// id. Remember only bounded identity strings, never credential values, so a
// second pass can redact those aliases independently of map or protobuf order.
func (p *payloadPreview) rememberIdentity(key, value string) {
	if !identityTraceKey(key) || value == "" || len(value) > tracePayloadMaxString || len(p.identities) >= tracePayloadMaxNodes {
		return
	}
	if p.identities == nil {
		p.identities = map[string]struct{}{}
	}
	p.identities[value] = struct{}{}
}

func (p *payloadPreview) rememberReflectIdentity(key string, value reflect.Value) {
	if !identityTraceKey(key) {
		return
	}
	for steps := 0; steps <= tracePayloadMaxDepth && value.IsValid(); steps++ {
		switch value.Kind() {
		case reflect.Interface, reflect.Pointer:
			if value.IsNil() {
				return
			}
			value = value.Elem()
		case reflect.String:
			p.rememberIdentity(key, value.String())
			return
		default:
			return
		}
	}
}

func (p *payloadPreview) redactIdentityIDs(value any) {
	if len(p.identities) == 0 {
		return
	}
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			if normalizedTraceKey(key) == "id" {
				if alias, ok := item.(string); ok {
					if _, private := p.identities[alias]; private {
						value[key] = traceRedacted
						p.redacted = true
						continue
					}
				}
			}
			p.redactIdentityIDs(item)
		}
	case []any:
		for _, item := range value {
			p.redactIdentityIDs(item)
		}
	}
}

func (p *payloadPreview) enter(depth int) bool {
	if depth > tracePayloadMaxDepth || p.nodes >= tracePayloadMaxNodes {
		p.truncated = true
		return false
	}
	p.nodes++
	return true
}

func normalizedTraceKey(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
}

func sensitiveTraceKey(key string) bool {
	key = normalizedTraceKey(key)
	for _, part := range []string{"password", "passwd", "passphrase", "secret", "token", "credential", "authorization", "cookie", "signature", "privatekey", "accesskey", "apikey", "sessionid", "sessionhash", "idempotencykey", "phone", "mobile", "telephone", "email", "recipient", "receiver", "address", "bankaccount", "bankcard", "cardnumber", "idcard", "passport"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	for _, part := range []string{"密码", "口令", "令牌", "密钥", "手机号", "电话号码", "收件人", "地址", "身份证", "银行卡", "邮箱", "用户名", "姓名"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	switch key {
	// Marketing delivery tokens identify browser views and recipient lists
	// contain private principals. Redact these exact keys at every JSON/RPC
	// boundary while keeping campaign IDs and targeting metadata useful.
	case "visitorid", "deliveryid", "viewid", "includeuserids", "excludeuserids":
		return true
	case "pwd", "auth", "session", "policy", "uploadfields", "downloadurl", "uploadurl", "presignedurl", "signedurl", "userid", "uid", "username", "ownerid", "memberid", "customerid", "accountid", "realname", "fullname", "displayname", "nickname", "contact", "province", "city", "district", "street", "postcode", "zipcode", "taxid", "taxnumber", "ip", "ipaddress", "filename", "body", "message", "content", "comment", "note", "reason", "question", "answer", "feedback", "reply":
		return true
	}
	return false
}

var (
	traceEmail         = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)+`)
	tracePhone         = regexp.MustCompile(`(?:\+?86[- ]?)?1[3-9][0-9]{9}`)
	traceBearer        = regexp.MustCompile(`(?i)\bBearer\s+[a-z0-9._~+/-]+=*`)
	traceURL           = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
	traceAssignment    = regexp.MustCompile(`(?i)(?:password|passwd|pwd|passphrase|secret|token|credential|authorization|cookie|api[_-]?key|access[_-]?key|private[_-]?key|密码|口令|密钥)["'\\]*\s*[:=]`)
	traceRawType       = reflect.TypeOf(json.RawMessage(nil))
	traceNumberType    = reflect.TypeOf(json.Number(""))
	traceTimeType      = reflect.TypeOf(time.Time{})
	errPreviewComplete = errors.New("trace preview limit reached")
)

func validPrefix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

func (p *payloadPreview) text(value string) string {
	// Signed URLs and credentials embedded in otherwise harmless string fields
	// must not survive merely because their enclosing key has a different name.
	if len(value) > tracePayloadMaxBytes {
		value = validPrefix(value, tracePayloadMaxBytes)
		p.truncated = true
	}
	if traceAssignment.MatchString(value) {
		p.redacted = true
		return traceRedacted
	}
	for _, candidate := range traceURL.FindAllString(value, -1) {
		if parsed, err := url.Parse(candidate); err == nil {
			if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				p.redacted = true
				return traceRedacted
			}
		}
	}
	for _, pattern := range []*regexp.Regexp{traceEmail, traceBearer} {
		if pattern.MatchString(value) {
			value = pattern.ReplaceAllString(value, traceRedacted)
			p.redacted = true
		}
	}
	// Numeric fragments of order/SKU identifiers are not phone numbers.
	var phoneSafe strings.Builder
	previous := 0
	identifier := func(char byte) bool {
		return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_'
	}
	for _, match := range tracePhone.FindAllStringIndex(value, -1) {
		if match[0] > 0 && identifier(value[match[0]-1]) || match[1] < len(value) && identifier(value[match[1]]) {
			continue
		}
		phoneSafe.WriteString(value[previous:match[0]])
		phoneSafe.WriteString(traceRedacted)
		previous = match[1]
		p.redacted = true
	}
	if previous > 0 {
		phoneSafe.WriteString(value[previous:])
		value = phoneSafe.String()
	}
	if len(value) > tracePayloadMaxString {
		p.truncated = true
		return validPrefix(value, tracePayloadMaxString) + "…"
	}
	return strings.ToValidUTF8(value, "�")
}

func traceJSONKey(key string) bool {
	key = normalizedTraceKey(key)
	return strings.HasSuffix(key, "json") || key == "payload" || key == "quote"
}

func (p *payloadPreview) value(value reflect.Value, key string, depth int) any {
	if sensitiveTraceKey(key) {
		p.redacted = true
		p.rememberReflectIdentity(key, value)
		return traceRedacted
	}
	if !p.enter(depth) {
		return traceTruncated
	}
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if !p.enter(depth) {
			return traceTruncated
		}
		if value.IsNil() {
			return nil
		}
		if value.CanInterface() {
			if message, ok := value.Interface().(proto.Message); ok {
				return p.message(message.ProtoReflect(), depth)
			}
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return nil
	}
	if value.Type() == traceRawType {
		return p.json(value.Bytes(), depth)
	}
	if value.Type() == traceNumberType {
		number := json.Number(value.String())
		if !json.Valid([]byte(number)) {
			p.truncated = true
			return "[INVALID NUMBER]"
		}
		return number
	}
	if value.Type() == traceTimeType {
		return value.Interface().(time.Time).Format(time.RFC3339Nano)
	}
	switch value.Kind() {
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value.Uint()
	case reflect.Float32, reflect.Float64:
		if math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) {
			p.truncated = true
			return "[NON-FINITE NUMBER]"
		}
		return value.Float()
	case reflect.String:
		raw := value.String()
		if len(raw) > tracePayloadMaxJSON {
			p.truncated = true
			return "[STRING OMITTED: SIZE LIMIT]"
		}
		trimmed := strings.TrimSpace(raw)
		if traceJSONKey(key) || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, `"`) {
			return p.json([]byte(raw), depth)
		}
		return p.text(raw)
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			if value.Kind() == reflect.Slice && traceJSONKey(key) {
				return p.json(value.Bytes(), depth)
			}
			p.redacted = true
			return "[BINARY OMITTED]"
		}
		items := make([]any, 0, min(value.Len(), tracePayloadMaxItems))
		for i := 0; i < min(value.Len(), tracePayloadMaxItems); i++ {
			items = append(items, p.value(value.Index(i), "", depth+1))
		}
		if value.Len() > len(items) {
			p.truncated = true
		}
		return items
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		if value.Type().Key().Kind() != reflect.String {
			p.truncated = true
			return "[UNSUPPORTED MAP]"
		}
		// Avoid collecting or sorting an unbounded map just for diagnostics.
		keys := make([]reflect.Value, 0, min(value.Len(), tracePayloadMaxFields))
		iter := value.MapRange()
		for len(keys) < tracePayloadMaxFields && iter.Next() {
			keys = append(keys, iter.Key())
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		result := make(map[string]any, len(keys))
		for _, field := range keys {
			name := field.String()
			result[p.text(name)] = p.value(value.MapIndex(field), name, depth+1)
		}
		if value.Len() > len(keys) {
			p.truncated = true
		}
		return result
	case reflect.Struct:
		result := map[string]any{}
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			if tag == "-" || (strings.Contains(options, "omitempty") && value.Field(i).IsZero()) {
				continue
			}
			if len(result) >= tracePayloadMaxFields {
				p.truncated = true
				break
			}
			if tag == "" {
				tag = field.Name
			}
			result[p.text(tag)] = p.value(value.Field(i), tag, depth+1)
		}
		return result
	default:
		p.truncated = true
		return "[UNSUPPORTED VALUE]"
	}
}

func (p *payloadPreview) json(raw []byte, depth int) any {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > tracePayloadMaxJSON {
		p.truncated = true
		return "[JSON OMITTED: SIZE LIMIT]"
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := p.readJSON(decoder, "", depth+1)
	if errors.Is(err, errPreviewComplete) {
		return value
	}
	if err == nil {
		var trailing any
		if trailingErr := decoder.Decode(&trailing); trailingErr != io.EOF {
			err = errors.New("trailing JSON")
		}
	}
	if err != nil {
		p.truncated = true
		return "[INVALID JSON OMITTED]"
	}
	return value
}

// Decode only the bounded preview of embedded JSON; do not materialize whole
// catalog/coupon results or convert opaque JSON bytes to base64 in telemetry.
func (p *payloadPreview) readJSON(decoder *json.Decoder, key string, depth int) (any, error) {
	if !p.enter(depth) {
		return traceTruncated, errPreviewComplete
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, collection := token.(json.Delim)
	if !collection {
		if text, ok := token.(string); ok {
			trimmed := strings.TrimSpace(text)
			if traceJSONKey(key) || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, `"`) {
				return p.json([]byte(text), depth), nil
			}
			return p.text(text), nil
		}
		return token, nil
	}
	if delimiter == '{' {
		result := map[string]any{}
		fields := 0
		for decoder.More() {
			if fields >= tracePayloadMaxFields {
				p.truncated = true
				return result, errPreviewComplete
			}
			fields++
			rawKey, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := rawKey.(string)
			if !ok {
				return nil, errors.New("invalid JSON key")
			}
			if sensitiveTraceKey(key) {
				p.redacted = true
				result[p.text(key)] = traceRedacted
				var discarded json.RawMessage
				if err := decoder.Decode(&discarded); err != nil {
					return nil, err
				}
				if identityTraceKey(key) && len(discarded) <= tracePayloadMaxString*6+2 {
					var identity string
					if json.Unmarshal(discarded, &identity) == nil {
						p.rememberIdentity(key, identity)
					}
				}
				continue
			}
			value, err := p.readJSON(decoder, key, depth+1)
			result[p.text(key)] = value
			if err != nil {
				return result, err
			}
		}
		_, err := decoder.Token()
		return result, err
	}
	if delimiter == '[' {
		result := []any{}
		for decoder.More() {
			if len(result) >= tracePayloadMaxItems {
				p.truncated = true
				return result, errPreviewComplete
			}
			value, err := p.readJSON(decoder, "", depth+1)
			result = append(result, value)
			if err != nil {
				return result, err
			}
		}
		_, err := decoder.Token()
		return result, err
	}
	return nil, errors.New("unexpected JSON delimiter")
}

func (p *payloadPreview) message(message protoreflect.Message, depth int) any {
	if !message.IsValid() {
		return nil
	}
	result := map[string]any{}
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if len(result) >= tracePayloadMaxFields || p.nodes >= tracePayloadMaxNodes {
			p.truncated = true
			return false
		}
		name := string(field.Name())
		if sensitiveTraceKey(name) {
			p.redacted = true
			if !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.StringKind {
				p.rememberIdentity(name, value.String())
			}
			result[name] = traceRedacted
		} else if field.IsList() {
			list := value.List()
			items := make([]any, 0, min(list.Len(), tracePayloadMaxItems))
			for i := 0; i < min(list.Len(), tracePayloadMaxItems); i++ {
				items = append(items, p.protoValue(field, list.Get(i), name, depth+1))
			}
			p.truncated = p.truncated || list.Len() > len(items)
			result[name] = items
		} else if field.IsMap() {
			items := map[string]any{}
			fields := 0
			value.Map().Range(func(key protoreflect.MapKey, item protoreflect.Value) bool {
				if fields >= tracePayloadMaxFields {
					p.truncated = true
					return false
				}
				fields++
				keyText := key.String()
				if sensitiveTraceKey(keyText) {
					p.redacted = true
					items[p.text(keyText)] = traceRedacted
				} else {
					items[p.text(keyText)] = p.protoValue(field.MapValue(), item, keyText, depth+1)
				}
				return true
			})
			result[name] = items
		} else {
			result[name] = p.protoValue(field, value, name, depth+1)
		}
		return true
	})
	return result
}

func (p *payloadPreview) protoValue(field protoreflect.FieldDescriptor, value protoreflect.Value, key string, depth int) any {
	if !p.enter(depth) {
		return traceTruncated
	}
	switch field.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return p.message(value.Message(), depth)
	case protoreflect.BytesKind:
		if traceJSONKey(key) {
			return p.json(value.Bytes(), depth)
		}
		p.redacted = true
		return "[BINARY OMITTED]"
	case protoreflect.StringKind:
		return p.value(reflect.ValueOf(value.String()), key, depth)
	case protoreflect.EnumKind:
		if descriptor := field.Enum().Values().ByNumber(value.Enum()); descriptor != nil {
			return string(descriptor.Name())
		}
		return int32(value.Enum())
	default:
		return p.value(reflect.ValueOf(value.Interface()), key, depth)
	}
}
