package usecase

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Import workbooks are data-only; formulas and external links are never evaluated.
func importFile(name string, raw []byte) ([][]string, error) {
	if len(raw) > 512*1024 {
		return nil, fmt.Errorf("文件不能超过512 KB")
	}
	if strings.EqualFold(path.Ext(name), ".csv") {
		if !utf8.Valid(raw) {
			return nil, fmt.Errorf("请使用UTF-8编码的CSV文件")
		}
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff")))
		r.FieldsPerRecord = -1
		rows := [][]string{}
		for {
			row, e := r.Read()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, fmt.Errorf("CSV格式错误：%v", e)
			}
			rows = append(rows, row)
			if len(rows) > 1001 {
				return nil, fmt.Errorf("每次最多1000行商品")
			}
		}
		return rows, nil
	}
	if !strings.EqualFold(path.Ext(name), ".xlsx") {
		return nil, fmt.Errorf("请选择CSV或XLSX文件")
	}
	z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		return nil, fmt.Errorf("XLSX文件无效")
	}
	files := map[string]*zip.File{}
	if len(z.File) > 2048 {
		return nil, fmt.Errorf("工作簿文件数量过多")
	}
	size := uint64(0)
	for _, f := range z.File {
		size += f.UncompressedSize64
		if size > 20*1024*1024 || f.UncompressedSize64 > 8*1024*1024 {
			return nil, fmt.Errorf("工作簿展开内容过大")
		}
		files[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f := files[name]
		if f == nil {
			return nil, fmt.Errorf("缺少工作表文件")
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		defer r.Close()
		v, e := io.ReadAll(io.LimitReader(r, 8*1024*1024+1))
		if len(v) > 8*1024*1024 {
			return nil, fmt.Errorf("工作表过大")
		}
		return v, e
	}
	shared := []string{}
	if files["xl/sharedStrings.xml"] != nil {
		v, e := read("xl/sharedStrings.xml")
		if e != nil {
			return nil, e
		}
		var root struct {
			Items []struct {
				Text string `xml:"t"`
				Runs []struct {
					Text string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if e = xml.Unmarshal(v, &root); e != nil {
			return nil, e
		}
		for _, item := range root.Items {
			s := item.Text
			for _, run := range item.Runs {
				s += run.Text
			}
			shared = append(shared, s)
		}
	}
	workbookXML, e := read("xl/workbook.xml")
	if e != nil {
		return nil, e
	}
	var workbook struct {
		Sheets []struct {
			ID string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if e = xml.Unmarshal(workbookXML, &workbook); e != nil || len(workbook.Sheets) == 0 {
		return nil, fmt.Errorf("工作簿缺少工作表")
	}
	relationsXML, e := read("xl/_rels/workbook.xml.rels")
	if e != nil {
		return nil, e
	}
	var relations struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
			Type   string `xml:"Type,attr"`
		} `xml:"Relationship"`
	}
	if e = xml.Unmarshal(relationsXML, &relations); e != nil {
		return nil, fmt.Errorf("工作表关系无效")
	}
	sheetPath := ""
	for _, rel := range relations.Items {
		if rel.ID != workbook.Sheets[0].ID {
			continue
		}
		if rel.Mode == "External" || !strings.HasSuffix(rel.Type, "/worksheet") {
			return nil, fmt.Errorf("首张工作表必须是普通数据表")
		}
		if strings.HasPrefix(rel.Target, "/") {
			sheetPath = path.Clean(strings.TrimPrefix(rel.Target, "/"))
		} else {
			sheetPath = path.Join("xl", rel.Target)
		}
		if !strings.HasPrefix(sheetPath, "xl/worksheets/") {
			return nil, fmt.Errorf("工作表路径无效")
		}
		break
	}
	if sheetPath == "" {
		return nil, fmt.Errorf("工作表关系缺失")
	}
	v, e := read(sheetPath)
	if e != nil {
		return nil, e
	}
	var sheet struct {
		Rows []struct {
			Index int `xml:"r,attr"`
			Cells []struct {
				Ref     string  `xml:"r,attr"`
				Type    string  `xml:"t,attr"`
				Value   string  `xml:"v"`
				Formula *string `xml:"f"`
				Inline  struct {
					Text string `xml:"t"`
					Runs []struct {
						Text string `xml:"t"`
					} `xml:"r"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if e = xml.Unmarshal(v, &sheet); e != nil {
		return nil, fmt.Errorf("工作表内容无效")
	}
	if len(sheet.Rows) > 1001 {
		return nil, fmt.Errorf("每次最多1000行商品")
	}
	out := [][]string{}
	for _, row := range sheet.Rows {
		cells := []string{}
		for _, cell := range row.Cells {
			if cell.Formula != nil {
				return nil, fmt.Errorf("第%d行含公式，请粘贴为值后导入", row.Index)
			}
			col := 0
			for _, ch := range cell.Ref {
				if ch < 'A' || ch > 'Z' {
					break
				}
				col = col*26 + int(ch-'A') + 1
			}
			if col < 1 || col > 32 {
				return nil, fmt.Errorf("工作表列数不能超过32列")
			}
			for len(cells) < col {
				cells = append(cells, "")
			}
			value := cell.Value
			if cell.Type == "s" {
				index, e := strconv.Atoi(value)
				if e != nil || index < 0 || index >= len(shared) {
					return nil, fmt.Errorf("无效文本索引")
				}
				value = shared[index]
			} else if cell.Type == "inlineStr" {
				value = cell.Inline.Text
				for _, run := range cell.Inline.Runs {
					value += run.Text
				}
			}
			cells[col-1] = value
		}
		out = append(out, cells)
	}
	return out, nil
}
