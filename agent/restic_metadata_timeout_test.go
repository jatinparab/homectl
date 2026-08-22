package agent

import (
	"context"
	"testing"
	"time"
)

func TestResticMetadataContextHasDeadline(t *testing.T) {
	ctx, cancel := resticMetadataContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("metadata context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < ResticMetadataTimeout-time.Second || remaining > ResticMetadataTimeout {
		t.Fatalf("metadata deadline remaining = %v, want approximately %v", remaining, ResticMetadataTimeout)
	}
}

func TestResticMetadataContextPreservesEarlierParentDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), time.Second)
	defer parentCancel()
	parentDeadline, _ := parent.Deadline()

	ctx, cancel := resticMetadataContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("metadata context has no deadline")
	}
	if deadline.After(parentDeadline) {
		t.Fatalf("metadata deadline %v exceeds parent deadline %v", deadline, parentDeadline)
	}
}
