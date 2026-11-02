package platform

// IndexResults decodes a bounded peer response without making remote calls.
func IndexResults(result Result, key string) (map[string]Result, error) {
	var data struct {
		Items []Result `json:"items"`
	}
	if err := Decode(result, &data); err != nil {
		return nil, err
	}
	rows := map[string]Result{}
	for _, row := range data.Items {
		rows[Text(row, key)] = row
	}
	return rows, nil
}
