package catalog

import (
	"bytes"
	"compress/zlib"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

func (d *DB) hasTable(ctx context.Context, name string) (bool, error) {
	var found string
	err := d.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// MissingRuntimeMetadataError explains that a browse-only or incomplete
// catalog cannot load a recovered runtime document.
type MissingRuntimeMetadataError struct {
	Path, Member, Detail string
}

func (e *MissingRuntimeMetadataError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return fmt.Sprintf("runtime metadata %q is not present in %s", e.Member, e.Path)
}

func (d *DB) missingRuntime(member, detail string) error {
	return &MissingRuntimeMetadataError{Path: d.Path, Member: member, Detail: detail}
}

func (d *DB) runtimeStoreError(member string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return d.missingDocument(member)
	}
	return fmt.Errorf("runtime metadata %s: %w; regenerate the SQLite catalog with `tools/gts bundle sqlite`", member, err)
}

func (d *DB) missingDocument(member string) error {
	what := fmt.Sprintf("runtime metadata %q is not present in this catalog", member)
	switch {
	case documentName(member) == "index.json":
		what = "catalog is missing index.json runtime metadata (needed to resolve vehicles, routes, and session/support tables)"
	case isURIMember(member) || strings.Contains(documentName(member), "pcs_data_viewer"):
		display := member
		if !strings.HasPrefix(display, "supplement://") && !strings.HasPrefix(display, "repo://") {
			display = "supplement://" + documentName(member)
		}
		what = fmt.Sprintf("catalog is missing %s (needed for FFD recorder semantics)", display)
	}
	return d.missingRuntime(member, what+"; regenerate the SQLite catalog with `tools/gts bundle sqlite`")
}

func documentName(member string) string {
	member = strings.TrimPrefix(member, "supplement://")
	member = strings.TrimPrefix(member, "repo://")
	return path.Base(member)
}

func isURIMember(member string) bool {
	return strings.HasPrefix(member, "supplement://") || strings.HasPrefix(member, "repo://")
}

func memberNamesMatch(stored, requested string) bool {
	if stored == requested {
		return true
	}
	if (isURIMember(stored) || isURIMember(requested)) && documentName(stored) != "" && documentName(stored) == documentName(requested) {
		return true
	}
	return false
}

func (d *DB) storedDocumentMembers(ctx context.Context) ([]string, error) {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	queries := []struct {
		table, sql string
	}{
		{"runtime_documents", "SELECT DISTINCT member FROM runtime_documents"},
		{"item_sources", "SELECT DISTINCT member FROM item_sources"},
		{"catalog_sources", "SELECT DISTINCT member FROM catalog_sources"},
	}
	for _, q := range queries {
		ok, err := d.hasTable(ctx, q.table)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		rows, err := d.db.QueryContext(ctx, q.sql)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				rows.Close()
				return nil, err
			}
			add(name)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return names, nil
}

func (d *DB) resolveDocumentMember(ctx context.Context, requested string) (string, error) {
	names, err := d.storedDocumentMembers(ctx)
	if err != nil {
		return "", err
	}
	for _, name := range names {
		if name == requested {
			return name, nil
		}
	}
	var matches []string
	for _, name := range names {
		if memberNamesMatch(name, requested) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", d.missingRuntime(requested, fmt.Sprintf("ambiguous runtime document %q matches %d stored members (%s); not guessing among them", requested, len(matches), strings.Join(matches, ", ")))
	}
	return requested, nil
}

func (d *DB) hasDocumentSources(ctx context.Context, member string) (bool, error) {
	checks := []struct {
		table, sql string
	}{
		{"item_sources", "SELECT 1 FROM item_sources WHERE member=? LIMIT 1"},
		{"catalog_sources", "SELECT 1 FROM catalog_sources WHERE member=? LIMIT 1"},
	}
	for _, q := range checks {
		ok, err := d.hasTable(ctx, q.table)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		var one int
		err = d.db.QueryRowContext(ctx, q.sql, member).Scan(&one)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	return false, nil
}

// Document reads a recovered catalog or supplement document. The public
// `tools/gts bundle sqlite` exporter stores row payloads and JSON pointers
// (and optional catalog_sources metadata), not a runtime_documents table.
// An optional attached source bundle is only consulted when those tables
// do not contain the member.
func (d *DB) Document(ctx context.Context, member string) (map[string]any, error) {
	resolved, err := d.resolveDocumentMember(ctx, member)
	if err != nil {
		return nil, err
	}
	if ok, err := d.hasTable(ctx, "runtime_documents"); err != nil {
		return nil, err
	} else if ok {
		var data []byte
		err = d.db.QueryRowContext(ctx, "SELECT data FROM runtime_documents WHERE member=?", resolved).Scan(&data)
		if err == nil {
			return d.documentFromSkeleton(ctx, resolved, data)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, d.runtimeStoreError(member, err)
		}
	}
	if has, err := d.hasDocumentSources(ctx, resolved); err != nil {
		return nil, err
	} else if has {
		return d.reconstructDocument(ctx, resolved)
	}
	if documentName(member) == "index.json" {
		return d.Index(ctx)
	}
	if d.bundle != nil {
		if doc, err := d.bundle.Document(member); err == nil {
			return doc, nil
		}
	}
	return nil, d.missingDocument(member)
}

func (d *DB) documentFromSkeleton(ctx context.Context, member string, data []byte) (map[string]any, error) {
	raw, err := inflateDocument(data)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err = json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, fmt.Errorf("runtime metadata %s is not an object", member)
	}
	return document, d.applyItemSources(ctx, member, document, false)
}

func (d *DB) reconstructDocument(ctx context.Context, member string) (map[string]any, error) {
	document := map[string]any{}
	if ok, err := d.hasTable(ctx, "catalog_sources"); err != nil {
		return nil, err
	} else if ok {
		var meta string
		err = d.db.QueryRowContext(ctx, "SELECT metadata FROM catalog_sources WHERE member=? LIMIT 1", member).Scan(&meta)
		if err == nil && meta != "" {
			var metadata map[string]any
			if err = json.Unmarshal([]byte(meta), &metadata); err != nil {
				return nil, fmt.Errorf("%s catalog metadata: %w", member, err)
			}
			applySourceMetadata(document, metadata)
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return document, d.applyItemSources(ctx, member, document, true)
}

func applySourceMetadata(document, metadata map[string]any) {
	if metadata == nil {
		return
	}
	for key, value := range metadata {
		if key == "sections" {
			continue
		}
		document[key] = value
	}
	sections, _ := metadata["sections"].(map[string]any)
	for name, extra := range sections {
		extraMap, _ := extra.(map[string]any)
		dest, _ := document[name].(map[string]any)
		if dest == nil {
			dest = map[string]any{}
			document[name] = dest
		}
		for key, value := range extraMap {
			if _, exists := dest[key]; !exists {
				dest[key] = value
			}
		}
	}
}

func (d *DB) applyItemSources(ctx context.Context, member string, document map[string]any, grow bool) error {
	ok, err := d.hasTable(ctx, "item_sources")
	if err != nil || !ok {
		return err
	}
	rows, err := d.db.QueryContext(ctx, "SELECT s.pointer,p.data FROM item_sources s LEFT JOIN item_payloads p ON p.item_id=s.item_id WHERE s.member=? ORDER BY s.item_id", member)
	if err != nil {
		return d.runtimeStoreError(member, err)
	}
	defer rows.Close()
	for rows.Next() {
		var pointer string
		var data []byte
		if err = rows.Scan(&pointer, &data); err != nil {
			return err
		}
		if pointer == "" {
			continue
		}
		raw, err := inflateDocument(data)
		if err != nil {
			return err
		}
		var value any
		if err = json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if err = applyDocumentPointer(document, pointer, value, grow); err != nil {
			return fmt.Errorf("%s: %w", member, err)
		}
	}
	return rows.Err()
}

func inflateDocument(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func setDocumentPointer(document map[string]any, pointer string, value any) error {
	return applyDocumentPointer(document, pointer, value, false)
}

func splitDocumentPointer(pointer string) ([]string, error) {
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("invalid source pointer %q", pointer)
	}
	parts := strings.Split(pointer[1:], "/")
	out := make([]string, len(parts))
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				if j+1 >= len(part) || (part[j+1] != '0' && part[j+1] != '1') {
					return nil, fmt.Errorf("invalid source pointer %q", pointer)
				}
				j++
			}
		}
		out[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return out, nil
}

func applyDocumentPointer(document map[string]any, pointer string, value any, grow bool) error {
	parts, err := splitDocumentPointer(pointer)
	if err != nil {
		return err
	}
	current := any(document)
	assign := func(any) {}
	for i, part := range parts {
		last := i == len(parts)-1
		switch node := current.(type) {
		case map[string]any:
			if last {
				if _, exists := node[part]; !exists && !grow {
					return fmt.Errorf("invalid source pointer %q", pointer)
				}
				node[part] = value
				return nil
			}
			child, exists := node[part]
			if !exists {
				if !grow {
					return fmt.Errorf("invalid source pointer %q", pointer)
				}
				child = newPointerChild(part, parts[i+1])
				node[part] = child
			}
			current = child
			key := part
			assign = func(v any) { node[key] = v }
		case []any:
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || strconv.Itoa(n) != part {
				return fmt.Errorf("invalid source pointer %q", pointer)
			}
			if n >= len(node) {
				if !grow {
					return fmt.Errorf("invalid source pointer %q", pointer)
				}
				grown := make([]any, n+1)
				copy(grown, node)
				node = grown
				assign(node)
			}
			if last {
				node[n] = value
				return nil
			}
			if node[n] == nil {
				if !grow {
					return fmt.Errorf("invalid source pointer %q", pointer)
				}
				node[n] = newPointerChild("", parts[i+1])
			}
			current = node[n]
			idx := n
			arr := node
			assign = func(v any) { arr[idx] = v }
		default:
			return fmt.Errorf("invalid source pointer %q", pointer)
		}
	}
	return nil
}

func newPointerChild(parentKey, next string) any {
	// Exporter maps keyed by packed numeric identities (dtcs) must not be
	// created as arrays just because the next pointer token is all digits.
	if parentKey == "dids" || parentKey == "dtcs" {
		return map[string]any{}
	}
	if _, ok := pointerIndex(next); ok {
		return []any{}
	}
	return map[string]any{}
}

func pointerIndex(part string) (int, bool) {
	n, err := strconv.Atoi(part)
	if err != nil || n < 0 || strconv.Itoa(n) != part {
		return 0, false
	}
	return n, true
}
