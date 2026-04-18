package merger

import (
	"math/big"
	"net"
	"sort"
)

type ipRange struct {
	start *big.Int
	end   *big.Int
	bits  int // 32 for IPv4, 128 for IPv6
}

// Merge takes a slice of IPNet, deduplicates and merges overlapping ranges,
// and returns a minimal set of CIDRs.
func Merge(nets []*net.IPNet) []*net.IPNet {
	if len(nets) == 0 {
		return nil
	}

	// Separate IPv4 and IPv6 ranges
	var v4ranges, v6ranges []ipRange
	for _, n := range nets {
		if n == nil {
			continue
		}
		ip := n.IP.Mask(n.Mask)
		if ip == nil {
			continue
		}
		ones, maskBits := n.Mask.Size()
		hostBits := uint(maskBits - ones)

		isV4 := ip.To4() != nil
		var startInt *big.Int
		var bits int
		if isV4 {
			bits = 32
			startInt = new(big.Int).SetBytes(ip.To4())
		} else {
			bits = 128
			startInt = new(big.Int).SetBytes(ip.To16())
		}

		endInt := new(big.Int).Or(startInt, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), hostBits), big.NewInt(1)))
		r := ipRange{start: startInt, end: endInt, bits: bits}

		if isV4 {
			v4ranges = append(v4ranges, r)
		} else {
			v6ranges = append(v6ranges, r)
		}
	}

	var result []*net.IPNet
	result = append(result, mergeRanges(v4ranges, 32)...)
	result = append(result, mergeRanges(v6ranges, 128)...)
	return result
}

func mergeRanges(ranges []ipRange, bits int) []*net.IPNet {
	if len(ranges) == 0 {
		return nil
	}

	// Sort by start
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start.Cmp(ranges[j].start) < 0
	})

	// Merge overlapping/adjacent
	merged := []ipRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		nextAfterLast := new(big.Int).Add(last.end, big.NewInt(1))
		if r.start.Cmp(nextAfterLast) <= 0 {
			if r.end.Cmp(last.end) > 0 {
				last.end = r.end
			}
		} else {
			merged = append(merged, r)
		}
	}

	// Convert ranges back to minimal CIDRs
	var nets []*net.IPNet
	for _, r := range merged {
		nets = append(nets, rangeToCIDRs(r.start, r.end, bits)...)
	}
	return nets
}

func rangeToCIDRs(start, end *big.Int, bits int) []*net.IPNet {
	var nets []*net.IPNet
	one := big.NewInt(1)
	byteLen := bits / 8

	for start.Cmp(end) <= 0 {
		// Size: block must fit within [start, end]
		rangeSize := new(big.Int).Add(new(big.Int).Sub(end, start), one)
		sizeBits := rangeSize.BitLen() - 1
		if sizeBits < 0 {
			sizeBits = 0
		}
		minSize := bits - sizeBits

		// Alignment: find the largest power-of-2 block that divides start
		// alignment = start & (-start), which isolates the lowest set bit
		negStart := new(big.Int).Neg(start)
		alignment := new(big.Int).And(start, negStart)
		alignBits := uint(alignment.BitLen() - 1)
		maxFromAlign := bits - int(alignBits)

		// Use the more restrictive (larger maskOnes = smaller block)
		maskOnes := minSize
		if maxFromAlign > minSize {
			maskOnes = maxFromAlign
		}

		mask := net.CIDRMask(maskOnes, bits)

		// Convert start big.Int to net.IP of correct byte length
		ipBytes := start.Bytes()
		ip := make(net.IP, byteLen)
		for i := 0; i < len(ipBytes) && i < byteLen; i++ {
			ip[byteLen-1-i] = ipBytes[len(ipBytes)-1-i]
		}

		nets = append(nets, &net.IPNet{IP: ip, Mask: mask})

		// Advance
		blockBitSize := uint(bits - maskOnes)
		start = new(big.Int).Add(start, new(big.Int).Lsh(one, blockBitSize))
	}
	return nets
}
