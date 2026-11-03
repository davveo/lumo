package domain

import "encoding/json"

// Record.Payload stays private on API responses. Durable jobs and revisions
// use an explicit codec so serializing them cannot silently drop product data.
type StoredRecord struct {
	Record
	Payload string `json:"payload"`
}

func StoredRecords(rows []Record) []StoredRecord {
	out := make([]StoredRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, StoredRecord{Record: r, Payload: r.Payload})
	}
	return out
}
func EncodeRecords(rows []Record) ([]byte, error) { return json.Marshal(StoredRecords(rows)) }
func DecodeRecords(data []byte) ([]Record, error) {
	var stored []StoredRecord
	if e := json.Unmarshal(data, &stored); e != nil {
		return nil, e
	}
	out := make([]Record, 0, len(stored))
	for _, r := range stored {
		r.Record.Payload = r.Payload
		out = append(out, r.Record)
	}
	return out, nil
}
func (r ImportRow) MarshalJSON() ([]byte, error) {
	type alias ImportRow
	return json.Marshal(struct {
		alias
		Records []StoredRecord `json:"records,omitempty"`
	}{alias: alias(r), Records: StoredRecords(r.Records)})
}
func (r *ImportRow) UnmarshalJSON(data []byte) error {
	type alias ImportRow
	var wire struct {
		alias
		Records []StoredRecord `json:"records,omitempty"`
	}
	if e := json.Unmarshal(data, &wire); e != nil {
		return e
	}
	*r = ImportRow(wire.alias)
	for _, row := range wire.Records {
		row.Record.Payload = row.Payload
		r.Records = append(r.Records, row.Record)
	}
	return nil
}
