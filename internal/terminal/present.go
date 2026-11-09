package terminal

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

// SearchSummary is "8 results", "first 50", or "no matches".
func summary(shown int, more bool) string {
	switch {
	case shown == 0:
		return "no matches"
	case more:
		return fmt.Sprintf("first %s", count(shown))
	}
	return plural(shown, "result", "results")
}

// itemTable is the one row layout for catalog records:
//
//	kind   ecu   identifier  name  detail                    #id
func (p Printer) itemTable(rows []catalog.Item, terms []string, showECU bool) {
	if p.narrow() {
		for _, r := range rows {
			owner := r.ECU
			if owner == "" {
				owner = "—"
			}
			p.printf("  %s  %s  %s  %s\n", p.kind(r.Kind), p.blue(owner), p.dim(r.Identifier), p.dim("#"+strconv.FormatInt(r.ID, 10)))
			p.printf("    %s\n", p.nameDetail(r.Name, r.Detail, p.Width-4, terms))
		}
		return
	}
	owners, idents, ids := make([]string, len(rows)), make([]string, len(rows)), make([]string, len(rows))
	for i, r := range rows {
		owners[i] = r.ECU
		if owners[i] == "" {
			owners[i] = "—"
		}
		idents[i] = r.Identifier
		ids[i] = "#" + strconv.FormatInt(r.ID, 10)
	}
	labels := make([]string, len(rows))
	for i, r := range rows {
		labels[i] = KindLabel(r.Kind)
	}
	kindW := max(7, colWidth(labels, 14))
	ecuW := 0
	if showECU {
		ecuW = colWidth(owners, 18)
	}
	identW := colWidth(idents, 12)
	idW := colWidth(ids, 9)
	fixed := 2 + kindW + 2 + identW + 2 + idW + 2
	if showECU {
		fixed += ecuW + 2
	}
	nameW := max(20, p.measure()-fixed)
	for i, r := range rows {
		var b strings.Builder
		b.WriteString("  " + pad(p.paint(fit(labels[i], kindW), kindInfo(r.Kind).color, false), kindW) + "  ")
		if showECU {
			b.WriteString(pad(p.paint(fit(owners[i], ecuW), theme.blue, false), ecuW) + "  ")
		}
		b.WriteString(pad(p.dim(fit(idents[i], identW)), identW) + "  ")
		name := p.nameDetail(r.Name, r.Detail, nameW, terms)
		b.WriteString(pad(name, nameW) + "  " + p.dim(padLeft(ids[i], idW)))
		p.println(strings.TrimRight(b.String(), " "))
	}
}

// Search prints ranked results across the whole catalog.
func (p Printer) Search(query, region string, rows []catalog.Item, more bool) {
	p.title(p.accent("search")+"  "+p.bold(query), p.dim(region+" · "+summary(len(rows), more)))
	p.println()
	if len(rows) == 0 {
		p.println("  Nothing found. Try a DID (0x1601 or 1601), a DTC (U0131), fewer words, or --region EU|JP.")
		return
	}
	p.itemTable(rows, Terms(query), true)
	if more {
		p.footer("--limit N shows more")
	}
}

// Scoped prints search results confined to one ECU.
func (p Printer) Scoped(e catalog.ECU, query string, rows []catalog.Item, more bool) {
	p.title(p.accent(e.Key)+"  "+p.bold(query), p.dim(e.Name+" · "+summary(len(rows), more)))
	p.println()
	if len(rows) == 0 {
		p.println("  Nothing in this ECU matches. toyo search " + query + " looks everywhere.")
		return
	}
	p.itemTable(rows, Terms(query), false)
	if more {
		p.footer("--limit N shows more")
	}
}

// Matches prints records that share an identifier within one ECU.
func (p Printer) Matches(e catalog.ECU, ident string, rows []catalog.Item) {
	p.title(p.accent(e.Key)+"  "+p.bold(ident), p.dim(e.Region+" · "+summary(len(rows), false)))
	p.println()
	p.itemTable(rows, nil, false)
	p.footer("toyo inspect ID shows one")
}

func ecuProfile(e catalog.ECU) string {
	family := strings.TrimPrefix(e.SupportMode, "p")
	if i := strings.IndexByte(e.SupportMode, '-'); i > 0 {
		family = strings.ToUpper(e.SupportMode[:i])
		if rest := e.SupportMode[i+1:]; rest != "standard" {
			family += " " + rest
		}
	} else if e.SupportMode != "" {
		family = e.SupportMode
	}
	return family
}

// ECU prints one ECU and the questions that can be asked of it.
func (p Printer) ECU(e catalog.ECU) {
	p.title(p.accent(e.Key)+"  "+p.bold(e.Name), p.dim(fmt.Sprintf("%s · category %d", e.Region, e.CategoryID)))
	p.println()
	var meta []string
	if prof := ecuProfile(e); prof != "" {
		meta = append(meta, prof)
	}
	if e.Generation != nil {
		meta = append(meta, fmt.Sprintf("generation %d", *e.Generation))
	}
	if e.Database != "" {
		meta = append(meta, e.Database)
	}
	if len(meta) > 0 {
		p.println("  " + p.dim(strings.Join(meta, " · ")))
		p.println()
	}
	if e.CatalogMember == "" {
		p.println("  " + p.dim("No decoded catalog for this ECU; it is declared in vehicle install sets only."))
		return
	}
	type row struct{ key, label, cmd, extra string }
	rows := []row{
		{"signals", "Data list", "data", ""},
		{"dtcs", "Trouble codes", "dtcs", ""},
		{"active_tests", "Active tests", "tests", ""},
		{"functions", "Functions", "functions", ""},
		{"plugins", "Plugins", "plugins", ""},
		{"utilities", "Utilities", "utilities", ""},
	}
	if n := e.Counts["dids"]; n > 0 {
		rows[0].extra = plural(n, "DID", "DIDs")
	}
	numW := 0
	for _, r := range rows {
		numW = max(numW, len(count(e.Counts[r.key])))
	}
	shown := false
	for _, r := range rows {
		n := e.Counts[r.key]
		if n == 0 {
			continue
		}
		shown = true
		line := fmt.Sprintf("  %s  %s", pad(r.label, 14), padLeft(count(n), numW))
		if r.extra != "" {
			line += "  " + p.dim(pad(r.extra, 9))
		} else {
			line += "  " + strings.Repeat(" ", 9)
		}
		if !p.NoHints {
			line += "  " + p.dim("toyo "+e.Key+" "+r.cmd)
		}
		p.println(strings.TrimRight(line, " "))
	}
	if !shown {
		p.println("  " + p.dim("Catalog is present but empty."))
	}
	p.footer("toyo "+e.Key+" IDENT decodes one record", "toyo monitor "+e.Key+" ITEM watches it live")
}

// ECUs prints a directory of ECUs. hidden counts matches that were left out
// because they have no decoded catalog.
func (p Printer) ECUs(region, query string, rows []catalog.ECU, hidden int) {
	meta := region + " · " + summary(len(rows), false)
	p.title(p.accent("ecus")+ifs(query != "", "  "+p.bold(query)), p.dim(meta))
	p.println()
	if len(rows) == 0 {
		p.println("  No ECUs match.")
		if hidden > 0 {
			p.footer(plural(hidden, "declared ECU", "declared ECUs") + " have no decoded catalog; --all lists them")
		}
		return
	}
	keys, names, cats := make([]string, len(rows)), make([]string, len(rows)), make([]string, len(rows))
	for i, e := range rows {
		keys[i], names[i], cats[i] = e.Key, e.Name, strconv.Itoa(e.CategoryID)
	}
	keyW, catW := colWidth(keys, 22), colWidth(cats, 6)
	stats := make([]string, len(rows))
	for i, e := range rows {
		if e.CatalogMember == "" {
			stats[i] = "declared only"
			continue
		}
		var parts []string
		for _, s := range []struct {
			key, label string
		}{{"signals", "data"}, {"dtcs", "dtc"}, {"active_tests", "tests"}} {
			if n := e.Counts[s.key]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s %s", count(n), s.label))
			}
		}
		stats[i] = strings.Join(parts, " · ")
	}
	statW := colWidth(stats, 34)
	nameW := max(16, p.measure()-2-keyW-2-catW-2-2)
	if !p.narrow() {
		nameW = max(16, min(colWidth(names, 40), p.measure()-2-keyW-2-catW-2-statW-4))
	}
	for i := range rows {
		line := "  " + pad(p.blue(fit(keys[i], keyW)), keyW) + "  " + pad(fit(names[i], nameW), nameW)
		line += "  " + p.dim(padLeft(cats[i], catW))
		if !p.narrow() {
			line += "  " + p.dim(fit(stats[i], statW))
		}
		p.println(strings.TrimRight(line, " "))
	}
	if hidden > 0 {
		p.footer(plural(hidden, "more ECU", "more ECUs") + " without decoded catalogs; --all lists them")
	}
}

var kindTitles = map[string]string{
	"did": "data list", "dtc": "trouble codes", "active-test": "active tests",
	"function": "functions", "plugin": "plugins", "utility": "utilities",
}

// Items prints one family of records belonging to an ECU.
func (p Printer) Items(e catalog.ECU, kind, query string, rows []catalog.Item, more bool) {
	label := kindTitles[kind]
	if label == "" {
		label = kind
	}
	p.title(p.accent(e.Key)+"  "+p.bold(label)+ifs(query != "", "  "+p.dim("matching ")+p.bold(query)),
		p.dim(e.Name+" · "+summary(len(rows), more)))
	p.println()
	if len(rows) == 0 {
		p.println("  No matches.")
		return
	}
	p.itemRows(rows, Terms(query))
	if more {
		p.footer("--limit N shows more")
	}
}

// itemRows is itemTable without the kind/ECU columns, for single-family lists.
func (p Printer) itemRows(rows []catalog.Item, terms []string) {
	if p.narrow() {
		p.itemTable(rows, terms, false)
		return
	}
	idents := make([]string, len(rows))
	for i, r := range rows {
		idents[i] = r.Identifier
	}
	identW := colWidth(idents, 14)
	nameW := max(20, p.measure()-2-identW-2)
	for i, r := range rows {
		line := "  " + pad(p.blue(fit(idents[i], identW)), identW) + "  " + p.nameDetail(r.Name, r.Detail, nameW, terms)
		p.println(line)
	}
}

// VehicleGroups prints vehicles grouped by OEM name.
func (p Printer) VehicleGroups(region, query string, groups []catalog.VehicleGroup, total int) {
	meta := plural(len(groups), "model", "models")
	if total > len(groups) {
		meta = fmt.Sprintf("first %d of %d models", len(groups), total)
	}
	p.title(p.accent("vehicles")+ifs(query != "", "  "+p.bold(query)), p.dim(region+" · "+meta))
	p.println()
	if len(groups) == 0 {
		p.println("  No vehicles match.")
		return
	}
	names := make([]string, len(groups))
	for i, g := range groups {
		if g.Name == "" {
			groups[i].Name = "(unnamed)"
		}
		names[i] = groups[i].Name
	}
	nameW := colWidth(names, 36)
	for _, g := range groups {
		var detail string
		if len(g.Types) == 1 {
			detail = strconv.Itoa(g.Types[0])
		} else {
			detail = fmt.Sprintf("%d variants · %d–%d", len(g.Types), g.Types[0], g.Types[len(g.Types)-1])
		}
		p.println("  " + pad(fit(g.Name, nameW), nameW) + "  " + p.dim(detail))
	}
	p.footer(ifs(total > len(groups), "--limit N shows more"), "toyo vehicle NAME lists its variants")
}

// VehicleVariants prints every Toyota vehicle type sharing one OEM name.
func (p Printer) VehicleVariants(region string, vehicles []catalog.Vehicle, ecus map[int]int) {
	name := vehicles[0].Name
	p.title(p.accent("vehicle")+"  "+p.bold(name), p.dim(region+" · "+plural(len(vehicles), "variant", "variants")))
	p.println()
	for _, v := range vehicles {
		sets := make([]string, len(v.InstallSetIDs))
		for i, s := range v.InstallSetIDs {
			sets[i] = strconv.Itoa(s)
		}
		p.println("  " + pad(p.blue(strconv.Itoa(v.Type)), 7) + "  " + pad(plural(ecus[v.Type], "ECU", "ECUs"), 9) + "  " + p.dim("install sets "+strings.Join(sets, " ")))
	}
	p.footer("toyo vehicle TYPE shows install-set ECUs")
}

// Vehicle prints one vehicle type and its declared install-set ECUs.
func (p Printer) Vehicle(v catalog.Vehicle, candidates []catalog.VehicleECU) {
	p.title(p.accent("vehicle")+"  "+p.bold(v.Name), p.dim(fmt.Sprintf("%s · type %d", v.Region, v.Type)))
	p.println()
	sets := make([]string, len(v.InstallSetIDs))
	for i, set := range v.InstallSetIDs {
		sets[i] = strconv.Itoa(set)
	}
	p.kv("Install sets", strings.Join(sets, " · "))
	p.println()
	p.section("ECUs", plural(len(candidates), "declared", "declared")+" in the install sets, not probed")
	p.println()
	if len(candidates) == 0 {
		p.println("  No indexed install-set rows.")
		return
	}
	keys, names, ends := make([]string, len(candidates)), make([]string, len(candidates)), make([]string, len(candidates))
	for i, e := range candidates {
		keys[i], names[i], ends[i] = e.Key, e.Name, "—"
		if keys[i] == "" {
			keys[i] = strconv.Itoa(e.CategoryID)
		}
		if e.RequestAddress != nil {
			ends[i] = fmt.Sprintf("0x%X", *e.RequestAddress)
			if e.AddressExtension != nil {
				ends[i] += fmt.Sprintf("/0x%02X", *e.AddressExtension)
			}
		}
	}
	keyW, nameW, endW := colWidth(keys, 20), colWidth(names, 30), colWidth(ends, 14)
	kindW := max(0, p.measure()-2-keyW-2-nameW-2-endW-2)
	for i, e := range candidates {
		line := "  " + pad(p.blue(fit(keys[i], keyW)), keyW) + "  " + pad(fit(names[i], nameW), nameW) + "  " + pad(p.dim(ends[i]), endW)
		if kindW > 8 && !p.narrow() {
			line += "  " + p.dim(fit(strings.ReplaceAll(e.TransportKind, "-", " "), kindW))
		}
		p.println(strings.TrimRight(line, " "))
	}
}

// Stats prints the catalog's provenance and size.
func (p Printer) Stats(dbPath string, s catalog.Stats) {
	p.title(p.accent("catalog")+"  "+p.bold("GTS+ "+s.Release), "")
	p.println()
	p.kv("File", dbPath)
	p.kv("Members", count(s.Members))
	p.kv("Records", count(s.Items))
	p.kv("Vehicles", count(s.Vehicles))
	p.kv("ECUs", count(s.ECUs))
}

// Verified prints the outcome of an integrity check.
func (p Printer) Verified() {
	p.println(p.accent("✓") + " Catalog integrity verified")
}

func ifs(ok bool, s string) string {
	if ok {
		return s
	}
	return ""
}

// Failure prints an error with whatever help it can offer. Plain terminals get
// a single greppable "toyo: …" line.
func (p Printer) Failure(err error) {
	msg := err.Error()
	if !utf8.ValidString(msg) {
		msg = strings.ToValidUTF8(msg, "�")
	}
	var nf *catalog.NotFoundError
	var amb *catalog.AmbiguousError
	mark := p.paint("✗", theme.red, true)
	if !p.Color {
		mark = "toyo:"
	}
	p.printf("%s %s\n", mark, msg)
	switch {
	case errors.As(err, &nf) && len(nf.Suggestions) > 0:
		cmds := make([]string, len(nf.Suggestions))
		for i, s := range nf.Suggestions {
			cmds[i] = p.blue(s)
		}
		p.printf("  %s %s\n", p.dim("did you mean"), strings.Join(cmds, p.dim(", ")))
	case errors.As(err, &amb):
		matches := append([]catalog.ECU(nil), amb.Matches...)
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].CatalogMember != "" && matches[j].CatalogMember == "" })
		for i, e := range matches {
			if i == 6 {
				p.printf("  %s\n", p.dim(fmt.Sprintf("… and %d more", len(matches)-i)))
				break
			}
			p.printf("  %s  %s  %s\n", pad(p.blue(e.Key), 18), pad(fit(e.Name, 30), 30), p.dim(strconv.Itoa(e.CategoryID)))
		}
		p.printf("  %s\n", p.dim("name one by key or category number"))
	}
}
