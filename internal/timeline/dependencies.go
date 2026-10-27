package timeline

import (
	"fmt"
	"strings"
)

// Dependencies draws only recorded direct-parent evidence. Trace membership or
// neighboring sequence numbers do not imply an execution dependency.
func Dependencies(tl *Timeline) string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	nodes := map[string]int{}
	for i, req := range tl.Requests {
		nodes[req.LogID] = i
		fmt.Fprintf(&b, "    n%d[\"Request %d · %d ms\"]\n", i, req.Seq, req.LatencyMs)
	}
	external := map[string]int{}
	for i, req := range tl.Requests {
		if req.TraceAssociation == nil {
			continue
		}
		seen := map[string]bool{}
		for _, parent := range req.TraceAssociation.Parents {
			if parent == req.LogID || seen[parent] {
				continue
			}
			seen[parent] = true
			if n, ok := nodes[parent]; ok {
				fmt.Fprintf(&b, "    n%d -.-> n%d\n", n, i)
			} else {
				n, ok := external[parent]
				if !ok {
					n = len(external)
					external[parent] = n
					fmt.Fprintf(&b, "    e%d[\"Parent outside this trace\"]\n", n)
				}
				fmt.Fprintf(&b, "    e%d -.-> n%d\n", n, i)
			}
		}
	}
	return b.String()
}
