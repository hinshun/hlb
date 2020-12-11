package progress

import (
	"fmt"

	"github.com/moby/buildkit/client"
	digest "github.com/opencontainers/go-digest"
)

type Tracker struct {
	vtxByDigest map[digest.Digest]*client.Vertex
}

func NewTracker() *Tracker {
	return &Tracker{
		vtxByDigest: make(map[digest.Digest]*client.Vertex),
	}
}

func (t *Tracker) Update(status *client.SolveStatus) error {
	for _, vtx := range status.Vertexes {
		_, ok := t.vtxByDigest[vtx.Digest]
		if !ok {
			t.vtxByDigest[vtx.Digest] = vtx
			// p.AddVertex(vtx)
		}
	}
	for _, s := range status.Statuses {
		_, ok := t.vtxByDigest[s.Vertex]
		if !ok {
			return fmt.Errorf("received status before vertex %s", s.Vertex)
		}
		// Update status somewhere.
	}
	for _, l := range status.Logs {
		_, ok := t.vtxByDigest[l.Vertex]
		if !ok {
			return fmt.Errorf("received log before vertex %s", l.Vertex)
		}
		// Write log somewhere.
		// p.AddVertexLog(l.Vertex, l)
	}
	return nil
}
