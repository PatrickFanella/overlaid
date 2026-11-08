package engine

import "strings"

// MaxRecentChoices bounds retained no-repeat memory across a pinned graph.
const MaxRecentChoices = MaxPinnedVersions * MaxSteps

// ValidRecentChoices validates private bounded no-repeat memory. Empty values
// mean the node has no earlier pick; nil means no memory was captured.
func ValidRecentChoices(recent map[string]string) bool {
	if len(recent) > MaxRecentChoices {
		return false
	}
	for key, child := range recent {
		automation, node, ok := strings.Cut(key, "/")
		if !ok || !validUUID(automation) || !validName(node) || child != "" && !validName(child) {
			return false
		}
	}
	return true
}

// CaptureRecentChoices copies only this graph's no-repeat nodes. Removed nodes
// are pruned at admission, bounding memory across version changes. A removed
// child is harmless: it excludes none of the current children.
func CaptureRecentChoices(g PinnedGraph, prior map[string]string) (map[string]string, error) {
	if g.Digest() == "" || !ValidRecentChoices(prior) {
		return nil, ErrRehearsal
	}
	out := map[string]string{}
	for _, version := range g.Versions() {
		d := version.Document()
		for _, node := range d.Nodes {
			if node.NoRepeat {
				key := d.AutomationID + "/" + node.ID
				out[key] = prior[key]
			}
		}
	}
	return out, nil
}
