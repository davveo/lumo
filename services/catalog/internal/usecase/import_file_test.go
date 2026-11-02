package usecase

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestImportFileReadsXLSXAndRejectsFormulaCells(t *testing.T) {
	workbook := func(formula bool) []byte {
		var buffer bytes.Buffer
		z := zip.NewWriter(&buffer)
		for name, content := range map[string]string{
			"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="商品" r:id="rId7"/></sheets></workbook>`,
			"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/products.xml"/></Relationships>`,
		} {
			f, _ := z.Create(name)
			_, _ = f.Write([]byte(content))
		}
		sheet, _ := z.Create("xl/worksheets/products.xml")
		price := "<v>1999</v>"
		if formula {
			price = "<f>1+1</f><v>2</v>"
		}
		_, _ = sheet.Write(
			[]byte(
				`<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>id</t></is></c><c r="B1" t="inlineStr"><is><t>name</t></is></c><c r="C1" t="inlineStr"><is><t>price_cents</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>xlsx-product</t></is></c><c r="B2" t="inlineStr"><is><r><t>示例</t></r><r><t>商品</t></r></is></c><c r="C2">` + price + `</c></row></sheetData></worksheet>`,
			),
		)
		_ = z.Close()
		return buffer.Bytes()
	}
	rows, e := importFile("products.xlsx", workbook(false))
	if e != nil || len(rows) != 2 || rows[1][2] != "1999" || rows[1][1] != "示例商品" {
		t.Fatal(rows, e)
	}
	if _, e = importFile("products.xlsx", workbook(true)); e == nil {
		t.Fatal("formula cell accepted")
	}
	if _, e = importFile("products.csv", []byte("id,name\nwrong,\"broken")); e == nil {
		t.Fatal("malformed CSV accepted")
	}
}
