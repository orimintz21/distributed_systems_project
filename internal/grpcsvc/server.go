// Package grpcsvc implements the KVStore gRPC service.
//
// STEP 4 — In Part I every RPC returns a DUMMY response: the goal is only to
// prove that the .proto compiles and that two processes can talk over gRPC.
// In Part II this type will apply operations to the state machine, and in
// Part III it will submit them to the replicated log.
package grpcsvc

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kvsv1 "github.com/your-team/kvs/gen/kvs/v1"
)

// Server implements kvsv1.KVStoreServer.
//
// Embedding UnimplementedKVStoreServer makes every RPC not written yet return
// codes.Unimplemented (HTTP 501 after translation): when you add RPCs for your
// own service, the project keeps compiling while you work through them.
type Server struct {
	kvsv1.UnimplementedKVStoreServer

	NodeID string // shown in dummy values, so you can see WHICH server answered
}

func New(nodeID string) *Server { return &Server{NodeID: nodeID} }

// dummyKeys is the pretend content of the store. The key "missing" never
// exists, so you can test the 404 path end to end.
var dummyKeys = []string{"alpha", "beta", "gamma"}

// The dummy store pretends that every existing key is at this version.
const dummyVersion = 1

// ---------------------------------------------------------------------------
// The example service (a key-value store). Replace it with your own service.
// ---------------------------------------------------------------------------

// Read is server streaming: one ReadResponse per entry, in sort order.
func (s *Server) Read(req *kvsv1.ReadRequest, stream kvsv1.KVStore_ReadServer) error {
	var keys []string
	switch sel := req.GetSelect().(type) {
	case *kvsv1.ReadRequest_Key: // exactly one key: missing is an error
		if sel.Key == "missing" {
			return status.Errorf(codes.NotFound, "key %q not found", sel.Key)
		}
		keys = []string{sel.Key}
	case *kvsv1.ReadRequest_Keys: // several keys: missing ones are skipped
		for _, k := range sel.Keys.GetKeys() {
			if k != "missing" {
				keys = append(keys, k)
			}
		}
	case *kvsv1.ReadRequest_Prefix:
		for _, k := range dummyKeys {
			if strings.HasPrefix(k, sel.Prefix) {
				keys = append(keys, k)
			}
		}
	case *kvsv1.ReadRequest_Range: // [from, to); an empty bound is open
		for _, k := range dummyKeys {
			if k >= sel.Range.GetFrom() && (sel.Range.GetTo() == "" || k < sel.Range.GetTo()) {
				keys = append(keys, k)
			}
		}
	case nil: // no selector: every key
		keys = append(keys, dummyKeys...)
	}

	// Sorting by key only; a real store sorts by req.Sort.Field as well.
	sort.Strings(keys)
	if req.GetSort().GetDescending() {
		for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
			keys[i], keys[j] = keys[j], keys[i]
		}
	}
	if n := int(req.GetLimit()); n > 0 && n < len(keys) {
		keys = keys[:n]
	}

	for _, k := range keys {
		e := &kvsv1.Entry{Key: k, Version: dummyVersion}
		if !req.GetKeysOnly() {
			e.Value = fmt.Sprintf("dummy from %s (%s read)", s.NodeID, req.GetConsistency())
		}
		if err := stream.Send(&kvsv1.ReadResponse{Entry: e}); err != nil {
			return err
		}
	}
	return nil
}

// Update is client streaming: the client sends one UpdateRequest per key and
// closes the stream; the server answers once, for the whole batch.
func (s *Server) Update(stream kvsv1.KVStore_UpdateServer) error {
	reqs, err := recvAll[kvsv1.UpdateRequest](stream.Recv)
	if err != nil {
		return err
	}
	if len(reqs) == 0 {
		return status.Error(codes.InvalidArgument, "no updates")
	}
	// Check the whole batch before "applying" anything: all or nothing.
	for _, r := range reqs {
		if r.IfVersion != nil && r.GetIfVersion() != dummyVersion {
			return status.Errorf(codes.FailedPrecondition,
				"key %q is at version %d, not %d", r.GetKey(), dummyVersion, r.GetIfVersion())
		}
	}
	out := &kvsv1.UpdateResponse{}
	for _, r := range reqs { // nothing is stored: compute the result and echo it
		var value string
		switch op := r.GetOp().(type) {
		case *kvsv1.UpdateRequest_Set:
			value = op.Set
		case *kvsv1.UpdateRequest_Append:
			value = "dummy" + op.Append // pretend the stored value was "dummy"
		case *kvsv1.UpdateRequest_Increment:
			value = fmt.Sprint(41 + op.Increment) // pretend it was 41
		default:
			return status.Errorf(codes.InvalidArgument, "key %q: no operation", r.GetKey())
		}
		out.Entries = append(out.Entries, &kvsv1.Entry{Key: r.GetKey(), Value: value, Version: dummyVersion + 1})
		out.Created = append(out.Created, r.GetKey() == "missing") // only "missing" is new
	}
	return stream.SendAndClose(out)
}

// Delete is client streaming as well: one DeleteRequest per key.
func (s *Server) Delete(stream kvsv1.KVStore_DeleteServer) error {
	reqs, err := recvAll[kvsv1.DeleteRequest](stream.Recv)
	if err != nil {
		return err
	}
	var deleted int32
	for _, r := range reqs {
		if r.IfVersion != nil && r.GetIfVersion() != dummyVersion {
			return status.Errorf(codes.FailedPrecondition,
				"key %q is at version %d, not %d", r.GetKey(), dummyVersion, r.GetIfVersion())
		}
		if r.GetKey() != "missing" {
			deleted++
		}
	}
	return stream.SendAndClose(&kvsv1.DeleteResponse{Deleted: deleted})
}

// recvAll reads a client stream until the client closes it (io.EOF).
func recvAll[T any](recv func() (*T, error)) ([]*T, error) {
	var all []*T
	for {
		m, err := recv()
		if errors.Is(err, io.EOF) {
			return all, nil
		}
		if err != nil {
			return nil, err
		}
		all = append(all, m)
	}
}
