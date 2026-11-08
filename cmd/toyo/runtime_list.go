package main

import (
	"encoding/json"
	"fmt"
	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
)

// Lists read bounded index rows, never every category document. Full provenance
// remains available through inspect using the catalog item ID.
func (r *runtimeApp) operationList(args []string) error {
	if len(args) > 3 {
		return usagef(args[0], "expected %s %s [ECU]", args[0], args[1])
	}
	kind := "active-test"
	if args[0] == "utility" {
		kind = "utility"
	}
	if args[1] == "groups" {
		kind = "active-test-group"
	}
	category := 0
	if len(args) == 3 {
		ecu, e := r.db.ECU(r.ctx, r.profile.Region, args[2])
		if e != nil {
			return e
		}
		category = ecu.CategoryID
		if r.profile.Vehicle != nil {
			selected, e := r.ecu(args[2])
			if e != nil {
				return e
			}
			category = selected.CategoryID
		}
	}
	limit, e := runtimeInt(r.opt.value("limit", "50"))
	if e != nil || limit < 1 || limit > 5000 {
		return fmt.Errorf("--limit must be between 1 and 5000")
	}
	items := []catalog.Item{}
	if category == 0 && r.profile.Vehicle != nil {
		seen := map[int]bool{}
		for _, ecu := range r.profile.ECUs {
			if seen[ecu.CategoryID] {
				continue
			}
			seen[ecu.CategoryID] = true
			rows, err := r.db.ListKind(r.ctx, r.profile.Region, ecu.CategoryID, kind, limit-len(items))
			if err != nil {
				return err
			}
			items = append(items, rows...)
			if len(items) == limit {
				break
			}
		}
	} else {
		items, e = r.db.ListKind(r.ctx, r.profile.Region, category, kind, limit)
		if e != nil {
			return e
		}
	}
	summaries := make([]map[string]any, 0, len(items))
	for _, item := range items {
		detail, e := r.db.Item(r.ctx, item.ID)
		if e != nil {
			return e
		}
		var row map[string]any
		if e = json.Unmarshal(detail.Payload, &row); e != nil {
			return e
		}
		summary := map[string]any{"catalog_item_id": item.ID, "ecu": item.ECU, "category_id": item.CategoryID, "identifier": item.Identifier, "name": item.Name, "kind": row["kind"], "execution": row["execution"]}
		if kind != "active-test-group" {
			plan := diagnostic.ResolvePlan(row)
			if kind == "utility" {
				plan = diagnostic.ResolveUtilityPlan(row)
			}
			summary["runtime_executable"] = plan.Executable()
			summary["materializable"] = diagnostic.CanMaterializeDirect(plan) || diagnostic.CanMaterializeRoutine(plan)
			summary["runtime_refusals"] = plan.Refusals
		}
		summaries = append(summaries, summary)
	}
	if r.opt.json {
		return r.output(summaries)
	}
	if len(summaries) == 0 {
		fmt.Println("No matches.")
		return nil
	}
	for _, s := range summaries {
		state := opText(s["execution"])
		if s["runtime_executable"] == true {
			state = "executable"
		} else if s["materializable"] == true {
			state = "materializable"
		}
		fmt.Printf("%-18v %-10v %-42v %s  #%v\n", s["ecu"], s["identifier"], s["name"], state, s["catalog_item_id"])
	}
	if len(items) == limit {
		fmt.Printf("\nShowing up to %d results; --limit N shows more.\n", limit)
	}
	fmt.Println("toyo inspect ID shows the full record.")
	return nil
}
