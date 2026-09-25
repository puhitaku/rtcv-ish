package emu

import (
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

// MaxBatchSize bounds the encoded size of the repeated field of one
// ApplyUnits or Write request, well below the 64 MiB frame cap.
const MaxBatchSize = 32 << 20

// batchBudget is MaxBatchSize, or the emulator's max_payload when smaller.
func (c *Client) batchBudget() int {
	budget := MaxBatchSize
	if c.info != nil {
		if mp := int(c.info.GetCapabilities().GetMaxPayload()); mp > 0 && mp < budget {
			budget = mp
		}
	}
	return budget
}

// fieldSize is the encoded size of m as an element of a repeated field.
func fieldSize(m proto.Message) int {
	return protowire.SizeTag(1) + protowire.SizeBytes(proto.Size(m))
}

// batches splits items, in order, into runs whose encoded size stays within
// budget. An item larger than budget gets a batch of its own.
func batches[T proto.Message](items []T, budget int) [][]T {
	var out [][]T
	start, size := 0, 0
	for i, it := range items {
		n := fieldSize(it)
		if i > start && size+n > budget {
			out = append(out, items[start:i])
			start, size = i, 0
		}
		size += n
	}
	if start < len(items) {
		out = append(out, items[start:])
	}
	return out
}

// splitChunk cuts a chunk whose encoded size exceeds budget into pieces
// at consecutive addresses that each fit.
func splitChunk(ch *emulatorv1.WriteChunk, budget int) []*emulatorv1.WriteChunk {
	if fieldSize(ch) <= budget {
		return []*emulatorv1.WriteChunk{ch}
	}
	// Room for the tag, the lengths and the other fields of each piece.
	overhead := fieldSize(&emulatorv1.WriteChunk{Domain: ch.GetDomain(), Address: ch.GetAddress()}) + 16
	step := max(budget-overhead, 1)
	data := ch.GetData()
	out := make([]*emulatorv1.WriteChunk, 0, (len(data)+step-1)/step)
	for off := 0; off < len(data); off += step {
		end := min(off+step, len(data))
		out = append(out, &emulatorv1.WriteChunk{Domain: ch.GetDomain(), Address: ch.GetAddress() + uint64(off), Data: data[off:end]})
	}
	return out
}
