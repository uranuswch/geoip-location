package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"log"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/inserter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/uranuswch/geoip-location/pkg/merger"
	"github.com/uranuswch/geoip-location/pkg/parser"
)

const (
	outputFile = "China-only.mmdb"

	// Data source URLs
	sapicsIPv4URL = "https://raw.githubusercontent.com/sapics/ip-location-db/main/geolite2-geo-whois-asn-country/geolite2-geo-whois-asn-country-ipv4.csv"
	sapicsIPv6URL = "https://raw.githubusercontent.com/sapics/ip-location-db/main/geolite2-geo-whois-asn-country/geolite2-geo-whois-asn-country-ipv6.csv"
	gaoyifanURL   = "https://raw.githubusercontent.com/gaoyifan/china-operator-ip/ip-lists/china46.txt"
	mon17URL      = "https://raw.githubusercontent.com/17mon/china_ip_list/master/china_ip_list.txt"
	qqwryRepo    = "metowolf/qqwry.dat"
)

func main() {
	var allNets []*net.IPNet

	// Source 1: sapics CSV (IPv4)
	fmt.Println("Fetching sapics IPv4 CSV...")
	data, err := fetch(sapicsIPv4URL)
	if err != nil {
		log.Printf("Warning: failed to fetch sapics IPv4: %v", err)
	} else {
		nets, err := parser.ParseSapicsCSV(data)
		if err != nil {
			log.Printf("Warning: failed to parse sapics IPv4: %v", err)
		} else {
			fmt.Printf("  Parsed %d CN IPv4 ranges\n", len(nets))
			allNets = append(allNets, nets...)
		}
	}

	// Source 1b: sapics CSV (IPv6)
	fmt.Println("Fetching sapics IPv6 CSV...")
	data, err = fetch(sapicsIPv6URL)
	if err != nil {
		log.Printf("Warning: failed to fetch sapics IPv6: %v", err)
	} else {
		nets, err := parser.ParseSapicsCSV(data)
		if err != nil {
			log.Printf("Warning: failed to parse sapics IPv6: %v", err)
		} else {
			fmt.Printf("  Parsed %d CN IPv6 ranges\n", len(nets))
			allNets = append(allNets, nets...)
		}
	}

	// Source 2: gaoyifan china46
	fmt.Println("Fetching gaoyifan china46...")
	data, err = fetch(gaoyifanURL)
	if err != nil {
		log.Printf("Warning: failed to fetch gaoyifan: %v", err)
	} else {
		nets, err := parser.ParseCIDRFile(data)
		if err != nil {
			log.Printf("Warning: failed to parse gaoyifan: %v", err)
		} else {
			fmt.Printf("  Parsed %d CIDRs\n", len(nets))
			allNets = append(allNets, nets...)
		}
	}

	// Source 3: 17mon china_ip_list
	fmt.Println("Fetching 17mon china_ip_list...")
	data, err = fetch(mon17URL)
	if err != nil {
		log.Printf("Warning: failed to fetch 17mon: %v", err)
	} else {
		nets, err := parser.ParseCIDRFile(data)
		if err != nil {
			log.Printf("Warning: failed to parse 17mon: %v", err)
		} else {
			fmt.Printf("  Parsed %d CIDRs\n", len(nets))
			allNets = append(allNets, nets...)
		}
	}

	// Source 4: qqwry.dat
	fmt.Println("Fetching qqwry.dat...")
	qqwryData, err := fetchQQwry()
	if err != nil {
		log.Printf("Warning: failed to fetch qqwry: %v", err)
	} else {
		nets, err := parser.ParseQQwry(qqwryData)
		if err != nil {
			log.Printf("Warning: failed to parse qqwry: %v", err)
		} else {
			fmt.Printf("  Parsed %d CN IP ranges\n", len(nets))
			allNets = append(allNets, nets...)
		}
	}

	fmt.Printf("\nTotal before merge: %d ranges\n", len(allNets))

	// Merge and deduplicate
	merged := merger.Merge(allNets)
	fmt.Printf("After merge: %d ranges\n", len(merged))

	// Generate MMDB
	if err := generateMMDB(merged, outputFile); err != nil {
		log.Fatalf("Failed to generate MMDB: %v", err)
	}

	// Generate SHA256
	if err := generateSHA256(outputFile); err != nil {
		log.Fatalf("Failed to generate checksum: %v", err)
	}

	fmt.Printf("\nGenerated %s and %s.sha256\n", outputFile, outputFile)
}

func fetch(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func fetchQQwry() ([]byte, error) {
	// Get the latest release asset from metowolf/qqwry.dat
	apiURL := "https://api.github.com/repos/metowolf/qqwry.dat/releases/latest"
	data, err := fetch(apiURL)
	if err != nil {
		return nil, err
	}

	// Find the download URL for the .dat file
	// Simple JSON parsing: look for "browser_download_url" ending in .dat
	datURL := extractDatURL(string(data))
	if datURL == "" {
		return nil, fmt.Errorf("no .dat file found in latest release")
	}

	fmt.Printf("  Downloading qqwry from: %s\n", datURL)
	return fetch(datURL)
}

func extractDatURL(jsonStr string) string {
	// Simple extraction without full JSON parser
	// Look for browser_download_url patterns
	const marker = `"browser_download_url":"`
	idx := 0
	for {
		i := indexOf(jsonStr[idx:], marker)
		if i < 0 {
			break
		}
		i += idx + len(marker)
		end := indexOf(jsonStr[i:], `"`)
		if end < 0 {
			break
		}
		url := jsonStr[i : i+end]
		if filepath.Ext(url) == ".dat" {
			return url
		}
		idx = i + end
	}
	return ""
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func generateMMDB(nets []*net.IPNet, filename string) error {
	writer, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoIP2-Country",
		RecordSize:              24,
		IPVersion:               6,
		DisableIPv4Aliasing:     false,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		return err
	}

	record := mmdbtype.Map{
		"country": mmdbtype.Map{
			"iso_code":   mmdbtype.String("CN"),
			"geoname_id": mmdbtype.Uint32(1814991),
		},
	}

	for _, n := range nets {
		if err := writer.InsertFunc(n, inserter.TopLevelMergeWith(record)); err != nil {
			log.Printf("Warning: failed to insert %s: %v", n, err)
		}
	}

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = writer.WriteTo(f)
	return err
}

func generateSHA256(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	checksum := fmt.Sprintf("%x  %s\n", h.Sum(nil), filename)
	return os.WriteFile(filename+".sha256", []byte(checksum), 0644)
}
