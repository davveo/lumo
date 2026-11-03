package commercepolicy

import (
	"testing"
	"time"
)

func TestPromiseRequiresConfigurationAndAddressAndHonorsCutoff(t *testing.T) {
	p := DefaultShipping()
	now := time.Date(2026, 11, 2, 6, 59, 0, 0, time.UTC)
	if p.Promise("上海", now)["available"] != false {
		t.Fatal("invented estimate")
	}
	p.PromiseEnabled = true
	p.Warehouse = "华东仓"
	p.CutoffHour = 15
	p.HandlingDays = 1
	p.TransitMinDays = 1
	p.TransitMaxDays = 3
	if p.Promise("", now)["available"] != false {
		t.Fatal("address required")
	}
	a := p.Promise("上海", now)
	b := p.Promise("上海", now.Add(time.Minute))
	if a["dispatch_by"] != "2026-11-03" || a["delivery_to"] != "2026-11-06" || b["dispatch_by"] != "2026-11-04" {
		t.Fatalf("cutoff wrong %v %v", a, b)
	}
	p.BlockedRegions = []string{"上海"}
	if p.Promise("上海", now)["available"] != false {
		t.Fatal("blocked region promised")
	}
}
