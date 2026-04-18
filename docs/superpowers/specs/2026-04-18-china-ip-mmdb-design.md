# China IP MMDB Database - Design Spec

## Goal

Build an automated pipeline that fetches China IP ranges from multiple open-source data sources, merges/deduplicates them, and generates a MaxMind MMDB database. Updated daily via GitHub Actions.

## Data Sources

| Source | URL | Format | IPv6 | Parse |
|--------|-----|--------|------|-------|
| sapics/ip-location-db | `geolite2-geo-whois-asn-country-ipv4.csv` + `*-ipv6.csv` | CSV: start_ip,end_ip,country_code | Yes | Filter CN column |
| gaoyifan/china-operator-ip | `china46.txt` (branch: ip-lists) | CIDR per line | Yes | net.ParseCIDR |
| 17mon/china_ip_list | `china_ip_list.txt` | CIDR per line | No | net.ParseCIDR |
| metowolf/qqwry.dat | GitHub release asset | QQWry binary (GBK) | No | Custom binary parser |

## Architecture

Single Go binary: download -> parse -> merge -> generate MMDB.

```
Source 1 (sapics CSV)    --\
Source 2 (gaoyifan CIDR) ---+-> Fetcher -> Parser -> []net.IPNet
Source 3 (17mon CIDR)    --+                        |
Source 4 (qqwry binary)  --/                        v
                                          Merger (union + dedup)
                                                    |
                                                    v
                                          mmdbwriter -> China-only.mmdb
```

## Project Structure

```
geoip-location/
├── cmd/geoip-gen/main.go   # Entry point
├── pkg/
│   ├── fetcher/             # Download sources
│   ├── parser/              # Parse each format
│   └── merger/              # Merge + dedup IP ranges
├── .github/workflows/update.yml
├── go.mod
└── go.sum
```

## MMDB Output

- Format: MaxMind GeoIP2 compatible, 24-bit record size
- Record: `{"country": {"iso_code": "CN", "geoname_id": 1814991}}`
- Includes IPv4 + IPv6
- Filename: `China-only.mmdb`
- Accompanied by `China-only.mmdb.sha256`

## GitHub Actions

- Schedule: daily UTC 00:00 (cron) + manual trigger (workflow_dispatch)
- Steps: checkout -> setup Go -> build -> run -> generate checksum -> push to release branch
- Only pushes if MMDB has changed (checksum comparison)

## Dependencies

- `github.com/maxmind/mmdbwriter` - MMDB writer
- QQWry parser: custom implementation (~200 lines)
