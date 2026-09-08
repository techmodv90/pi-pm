package apm

import (
	"os"

	"github.com/earendil-works/task-system/go-pic/internal/store"
)

type FeatureGraph struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	TaskTIDs []string `json:"task_ids"`
}

type apmGraph struct {
	EpicName             string         `json:"epic_name"`
	MilestoneLabel       string         `json:"milestone_label"`
	VerificationCommands []string       `json:"verification_commands"`
	Features             []FeatureGraph `json:"features"`
	Tasks                []TaskGraph    `json:"tasks"`
	Edges                []EdgeGraph    `json:"edges"`
}

type TaskGraph struct {
	TID        string   `json:"tid"`
	Feature    string   `json:"feature"`
	Verbatim   string   `json:"description"`
	Acceptance string   `json:"acceptance"`
	US         string   `json:"us,omitempty"`
	DependsOn  []string `json:"depends_on"`
}

type EdgeGraph struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// BuildGraph groups tasks into P-tier features (Phase 1+2+P1→F1,
// P2→F2, P3+polish→F3) and derives edges: Rule A chains the features, Rule B
// sequences tasks inside a feature by the Execution Order block, Rule C gives
// parallel tasks the same predecessor set. Task edges never cross features.
func BuildGraph(doc *Doc, milestone string) *apmGraph {
	graph := &apmGraph{
		EpicName:       doc.Name,
		MilestoneLabel: "milestone:" + milestone,
	}
	featureOf := map[string]string{}
	addTo := func(key, name string, t Task) {
		for i := range graph.Features {
			if graph.Features[i].Key == key {
				graph.Features[i].TaskTIDs = append(graph.Features[i].TaskTIDs, t.TID)
				featureOf[t.TID] = key
				return
			}
		}
		graph.Features = append(graph.Features, FeatureGraph{Key: key, Name: name, TaskTIDs: []string{t.TID}})
		featureOf[t.TID] = key
	}
	f1Name := doc.TierHeaders["P1"]
	f2Name := doc.TierHeaders["P2"]
	f3Name := doc.TierHeaders["P3"]
	for _, t := range doc.Tasks {
		switch {
		case t.Phase <= 2 || t.Tier == "P1":
			addTo("F1", f1Name, t)
		case t.Tier == "P2":
			addTo("F2", f2Name, t)
		case t.Tier == "P3" || t.Phase == 4:
			addTo("F3", f3Name, t)
		case t.Phase == 5:
			graph.VerificationCommands = append(graph.VerificationCommands, t.Verbatim)
		}
	}
	// Flatten the order block into a global task sequence for predecessor sets.
	var seq []string
	for _, ol := range doc.Order {
		seq = append(seq, ol.Seq...)
	}
	pos := map[string]int{}
	for i, tid := range seq {
		pos[tid] = i
	}
	// Rule C: tasks joined by ║ on one order line are parallel — they share
	// the predecessor set of everything strictly before that line, and must
	// not depend on each other.
	parallelRank := map[string]int{}
	for _, ol := range doc.Order {
		if ol.Parallel {
			for _, tid := range ol.Seq {
				parallelRank[tid] = pos[ol.Seq[0]]
			}
		}
	}
	predecessors := func(tid string) []string {
		tidRank, ok := parallelRank[tid]
		if !ok {
			tidRank = pos[tid]
		}
		var preds []string
		for _, other := range seq {
			rank := pos[other]
			if r, ok := parallelRank[other]; ok {
				rank = r
			}
			if rank < tidRank && featureOf[other] == featureOf[tid] {
				preds = append(preds, other)
			}
		}
		return preds
	}
	for _, t := range doc.Tasks {
		if featureOf[t.TID] == "" {
			continue
		}
		graph.Tasks = append(graph.Tasks, TaskGraph{
			TID:        t.TID,
			Feature:    featureOf[t.TID],
			Verbatim:   t.Verbatim,
			Acceptance: t.Acceptance,
			US:         t.US,
			DependsOn:  predecessors(t.TID),
		})
	}
	if len(graph.Features) == 3 {
		graph.Edges = append(graph.Edges, EdgeGraph{From: "F2", To: "F1"}, EdgeGraph{From: "F3", To: "F2"})
	}
	return graph
}

func writeGraphJSON(graph *apmGraph, imported bool, epicID string) {
	payload := map[string]any{
		"epic": map[string]any{
			"name":                  graph.EpicName,
			"milestone_label":       graph.MilestoneLabel,
			"verification_commands": graph.VerificationCommands,
		},
		"features": graph.Features,
		"tasks":    graph.Tasks,
		"edges":    graph.Edges,
		"imported": imported,
	}
	if epicID != "" {
		payload["epic_id"] = epicID
	}
	store.WriteJSON(os.Stdout, payload)
}
