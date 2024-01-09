package bdkwallet

import (
	"path/filepath"
	"testing"

	"github.com/utreexo/utreexod/chaincfg"
)

func TestCreateAndLoad(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bdk.db")

	{
		wallet, err := Create(dbPath, &chaincfg.MainNetParams)
		if err != nil {
			t.Fatalf("failed to create db: %v", err)
		}

		freshI, freshAddr, err := wallet.FreshAddress()
		if err != nil {
			t.Fatalf("failed to get fresh address: %v", err)
		}
		if freshI != 0 {
			t.Fatal("fresh address of new wallet should be 0")
		}
		t.Logf("address at index 0: %v", freshAddr)

		unusedI, unusedAddr, err := wallet.UnusedAddress()
		if err != nil {
			t.Fatalf("failed to get unused address: %v", err)
		}
		if unusedI != freshI {
			t.Fatalf("the fresh address is not used so unused index should be 0")
		}
		if unusedAddr.String() != freshAddr.String() {
			t.Fatalf("unused addr should be the same as fresh addr: unused=%v", unusedAddr)
		}

		// derive 4 more addresses (total 5 addresses)
		for i := 1; i < 5; i++ {
			addrI, _, err := wallet.FreshAddress()
			if err != nil {
				t.Fatalf("failed to derive addr %v: %v", i, err)
			}
			if i != int(addrI) {
				t.Fatalf("derived addr index is unexpected")
			}
		}
	}

	// load wallet and see what happens
	{
		wallet, err := Load(dbPath)
		if err != nil {
			t.Fatalf("failed to load wallet: %v", err)
		}

		addrI, _, err := wallet.FreshAddress()
		if err != nil {
			t.Fatalf("failed to get fresh addr: %v", err)
		}

		if addrI != 5 {
			t.Fatalf("this should be 5: %v", addrI)
		}
	}
}
