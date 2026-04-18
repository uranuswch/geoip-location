package parser

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// ParseCIDRFile parses a file with one CIDR per line (e.g., 1.0.1.0/24).
func ParseCIDRFile(data []byte) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		_, ipNet, err := net.ParseCIDR(line)
		if err != nil {
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets, nil
}

// ParseSapicsCSV parses sapics CSV format: start_ip,end_ip,country_code
func ParseSapicsCSV(data []byte) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		country := strings.TrimSpace(parts[2])
		if country != "CN" {
			continue
		}
		startIP := net.ParseIP(strings.TrimSpace(parts[0]))
		endIP := net.ParseIP(strings.TrimSpace(parts[1]))
		if startIP == nil || endIP == nil {
			continue
		}
		ipNets := ipRangeToCIDRs(startIP, endIP)
		nets = append(nets, ipNets...)
	}
	return nets, nil
}

// ipRangeToCIDRs converts an IP range to a list of CIDRs using big.Int for IPv4/IPv6.
func ipRangeToCIDRs(start, end net.IP) []*net.IPNet {
	start = start.To16()
	end = end.To16()
	if start == nil || end == nil {
		return nil
	}

	isIPv4 := start.To4() != nil
	var bits int
	if isIPv4 {
		bits = 32
		start = start.To4()
		end = end.To4()
	} else {
		bits = 128
	}

	startInt := new(big.Int).SetBytes(start)
	endInt := new(big.Int).SetBytes(end)
	one := big.NewInt(1)
	byteLen := bits / 8

	var nets []*net.IPNet
	for startInt.Cmp(endInt) <= 0 {
		// Size constraint
		rangeSize := new(big.Int).Add(new(big.Int).Sub(endInt, startInt), one)
		sizeBits := rangeSize.BitLen() - 1
		if sizeBits < 0 {
			sizeBits = 0
		}
		minSize := bits - sizeBits

		// Alignment constraint
		negStart := new(big.Int).Neg(startInt)
		alignment := new(big.Int).And(startInt, negStart)
		alignBits := uint(alignment.BitLen() - 1)
		maxFromAlign := bits - int(alignBits)

		maskOnes := minSize
		if maxFromAlign > minSize {
			maskOnes = maxFromAlign
		}

		mask := net.CIDRMask(maskOnes, bits)

		ipBytes := startInt.Bytes()
		ip := make(net.IP, byteLen)
		for i := 0; i < len(ipBytes) && i < byteLen; i++ {
			ip[byteLen-1-i] = ipBytes[len(ipBytes)-1-i]
		}

		nets = append(nets, &net.IPNet{IP: ip, Mask: mask})

		blockSize := new(big.Int).Lsh(one, uint(bits-maskOnes))
		startInt.Add(startInt, blockSize)
	}
	return nets
}

// ParseQQwry parses the QQWry binary format and returns IP ranges for China entries.
func ParseQQwry(data []byte) ([]*net.IPNet, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("qqwry: data too short")
	}

	firstIdx := binary.LittleEndian.Uint32(data[0:4])
	lastIdx := binary.LittleEndian.Uint32(data[4:8])

	if firstIdx > lastIdx || lastIdx >= uint32(len(data)) {
		return nil, fmt.Errorf("qqwry: invalid index offsets")
	}

	var nets []*net.IPNet
	for idx := firstIdx; idx <= lastIdx; idx += 7 {
		if idx+7 > uint32(len(data)) {
			break
		}
		startIP := data[idx : idx+4]
		recordOffset := readUint24(data[idx+4 : idx+7])

		if recordOffset+4 > uint32(len(data)) {
			continue
		}
		endIP := data[recordOffset : recordOffset+4]

		country, _, ok := readQQwryRecord(data, recordOffset+4)
		if !ok || country == "" {
			continue
		}

		if isChinaCountry(country) {
			sip := net.IPv4(startIP[0], startIP[1], startIP[2], startIP[3])
			eip := net.IPv4(endIP[0], endIP[1], endIP[2], endIP[3])
			// Skip overly broad ranges (larger than /16) which are catch-all/aggregate entries
			sipVal := uint(startIP[0])<<24 | uint(startIP[1])<<16 | uint(startIP[2])<<8 | uint(startIP[3])
			eipVal := uint(endIP[0])<<24 | uint(endIP[1])<<16 | uint(endIP[2])<<8 | uint(endIP[3])
			if eipVal-sipVal >= 1<<16 {
				continue
			}
			nets = append(nets, ipRangeToCIDRs(sip, eip)...)
		}
	}
	return nets, nil
}

func readUint24(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}

func readQQwryRecord(data []byte, offset uint32) (country, area string, ok bool) {
	if offset >= uint32(len(data)) {
		return "", "", false
	}

	mode := data[offset]

	switch mode {
	case 0x01:
		if offset+4 > uint32(len(data)) {
			return "", "", false
		}
		redirOff := readUint24(data[offset+1 : offset+4])
		if redirOff >= uint32(len(data)) {
			return "", "", false
		}
		redirMode := data[redirOff]
		if redirMode == 0x02 {
			if redirOff+4 > uint32(len(data)) {
				return "", "", false
			}
			country = readQQwryString(data, readUint24(data[redirOff+1:redirOff+4]))
			area = readQQwryArea(data, redirOff+4)
		} else {
			country = readQQwryString(data, redirOff)
			area = readQQwryArea(data, 0) // simplified
		}
	case 0x02:
		if offset+4 > uint32(len(data)) {
			return "", "", false
		}
		countryOff := readUint24(data[offset+1 : offset+4])
		country = readQQwryString(data, countryOff)
		area = readQQwryArea(data, offset+4)
	default:
		country = readQQwryString(data, offset)
	}
	return country, area, true
}

func readQQwryArea(data []byte, offset uint32) string {
	if offset >= uint32(len(data)) {
		return ""
	}
	if data[offset] == 0x02 {
		if offset+4 > uint32(len(data)) {
			return ""
		}
		areaOff := readUint24(data[offset+1 : offset+4])
		return readQQwryString(data, areaOff)
	}
	return readQQwryString(data, offset)
}

func readQQwryString(data []byte, offset uint32) string {
	if offset >= uint32(len(data)) {
		return ""
	}
	var end int
	for end = int(offset); end < len(data) && data[end] != 0; end++ {
	}
	raw := data[offset:end]
	decoded, err := io.ReadAll(transform.NewReader(strings.NewReader(string(raw)), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return string(raw)
	}
	return string(decoded)
}

func isChinaCountry(s string) bool {
	s = strings.TrimSpace(s)
	return s == "中国" || strings.HasPrefix(s, "中国") || s == "纯真网络"
}
