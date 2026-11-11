package catalog

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

func TestShippedCatalogOccurrencesLieInsideOwnerRanges(t *testing.T) {
	d := openShipped(t)
	if err := verifyOwnerRanges(context.Background(), d.db); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsOccurrencesOutsideOwnerRanges(t *testing.T) {
	const wide = `(region_id,category_id,first_id)=(SELECT region_id,category_id,first_id FROM owner_ranges WHERE last_id>first_id LIMIT 1)`
	for name, change := range map[string]string{
		"outside":  `UPDATE owner_ranges SET last_id=last_id-1 WHERE ` + wide,
		"overlap":  `INSERT INTO owner_ranges SELECT region_id,category_id,first_id+1,last_id FROM owner_ranges WHERE ` + wide,
		"no range": `DELETE FROM owner_ranges WHERE ` + wide,
	} {
		t.Run(name, func(t *testing.T) {
			path := fixtureCatalog(t).Path()
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := db.Exec(change); err != nil {
				t.Fatal(err)
			} else if n, _ := result.RowsAffected(); n != 1 {
				t.Fatalf("changed %d owner ranges", n)
			}
			db.Close()
			d, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err := d.Verify(context.Background()); err == nil || !strings.Contains(err.Error(), "owner ranges") {
				t.Fatalf("Verify = %v", err)
			}
		})
	}
}

func TestSearchScopeEndingAtZeroStaysFound(t *testing.T) {
	c := catalogtest.New(t)
	c.ECU(catalogtest.ECU{Category: 7, Key: "abs", Name: "Brakes", Database: "ABS"})
	c.Item(catalogtest.Item{ID: -1, Category: 7, Kind: "did", Identifier: "0x01", Name: "Wheel speed"},
		catalogtest.Item{ID: 0, Category: 7, Kind: "did", Identifier: "0x02", Name: "Wheel slip"})
	d := openFixture(t, c)
	for range 2 {
		rows, err := d.SearchKind(context.Background(), "NA", 7, "", "wheel", 10)
		if err != nil || len(rows) != 2 {
			t.Fatalf("%v %v", identifiers(rows), err)
		}
	}
}

// An ECU's own record ranks first when the query names the ECU, whatever
// the record itself is called.
func TestSearchPromotesECURecordByECUName(t *testing.T) {
	c := catalogtest.New(t)
	c.ECU(catalogtest.ECU{Category: 405, Key: "eps", Name: "Power Steering", Database: "EMPS"})
	c.Item(catalogtest.Item{ID: 1, Category: 405, Kind: "did", Identifier: "0x10", Name: "Power Steering"},
		catalogtest.Item{ID: 2, Category: 405, Kind: "ecu", Identifier: "405", Name: "Steering power unit"})
	d := openFixture(t, c)
	rows, err := d.Search(context.Background(), "NA", "power steering", 10)
	if err != nil || len(rows) != 2 || rows[0].Kind != "ecu" {
		t.Fatalf("%+v %v", rows, err)
	}
}
