// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstats

import (
	"bytes"
	"fmt"
	"net/http"
)

const (
	tblHeader  = `<table border="1">`
	tblFooter  = `</table></body><br><br>`
	bodyFooter = `</body><br><br>`
	statHeader = `<th>Allocated</th><th>Freed</th><th>Allocated %</th><th>Freed %</th>`
	statFmt    = "<td>%d</td><td>%d</td><td>%.2f%%</td><td>%.2f%%</td>"
)

// htmlWrite writes the memory statistics of tango in html format
func htmlWrite(w http.ResponseWriter, r *http.Request, sec int64, res *memStat) {
	var b bytes.Buffer
	b.WriteString(`<html><head><title>Memory Statistics</title></head>`)
	b.WriteString("<body><tr>Memory Statisticss:</tr><br>")

	if sec > 0 {
		fmt.Fprintf(&b, "<tr>  Duration: %d seconds</tr><br>", sec)
	}

	fmt.Fprintf(&b, "<tr>  GAB Allocator: size limit=%dBytes, status=%s</tr>",
		gabMaxAllocSize(), gabStatus())
	b.WriteString(tblFooter)

	// #Overview
	desc := "Heap Objects: Overview"
	writeHtmlSummaryTable1(&b, desc, res.mallocs, res.frees, 100.0, 100.0)

	// #Gab
	desc = "Heap Objects: Candidates for GAB allocator"
	writeHtmlSummaryTable2(&b, desc, &res.gab)

	// #Large object stats
	desc = "Heap Objects: Large, size > 32KB"
	writeHtmlSummaryTable2(&b, desc, &res.large)

	// #Small object stats
	desc = "Heap Objects: Small, size <= 32KB"
	writeHtmlSummaryTable2(&b, desc, &res.small)

	// #Tiny object stats
	desc = "Heap Objects: Tiny, size < 16B && no-scan, aggregated in a small-object slot"
	writeHtmlSummaryTable2(&b, desc, &res.tiny)

	// #Small object details
	b.WriteString(`Heap Objects: Details of small objects per size class<br>`)
	b.WriteString(tblHeader)
	b.WriteString(`<tr><th rowspan="2">Size</th><th colspan="4">No-Scan Objects</th>
                      <th colspan="4">Scan Objects</th></tr>`)
	b.WriteString("<tr>")
	b.WriteString(statHeader)
	b.WriteString(statHeader)
	b.WriteString("</tr>")

	for i := 0; i < len(res.smalls); i++ {
		b.WriteString("<tr>")
		fmt.Fprintf(&b, "<td>%d</td>", res.smalls[i].size)
		a, f, ap, fp := res.smalls[i].noScanStats()
		fmt.Fprintf(&b, statFmt, a, f, ap, fp)
		a, f, ap, fp = res.smalls[i].scanStats()
		fmt.Fprintf(&b, statFmt, a, f, ap, fp)
		b.WriteString("</tr>")
	}
	b.WriteString(tblFooter)

	b.WriteString(`</html>`)
	w.Write(b.Bytes())
}

func writeHtmlSummaryTable1(b *bytes.Buffer, desc string, nsa, nsf uint64, nsap, nsfp float64) {
	fmt.Fprintf(b, "<body>%s<br>", desc)
	b.WriteString(tblHeader)
	b.WriteString("<tr>")
	b.WriteString(statHeader)
	b.WriteString("</tr>")
	b.WriteString("<tr>")
	fmt.Fprintf(b, statFmt, nsa, nsf, nsap, nsfp)
	b.WriteString("</tr>")
	b.WriteString(tblFooter)
}

func writeHtmlSummaryTable2(b *bytes.Buffer, desc string, stat *objStat) {
	nsa, nsf, nsap, nsfp := stat.noScanStats()
	writeHtmlSummaryTable1(b, desc, nsa, nsf, nsap, nsfp)
}
