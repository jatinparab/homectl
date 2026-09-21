package agent

import (
	"reflect"
	"testing"
)

func TestBootstrapDNSFromEnv(t *testing.T) {
	t.Setenv("HOMECTL_BOOTSTRAP_DNS", "1.1.1.1, 9.9.9.9;1.1.1.1")
	want := []string{"1.1.1.1", "9.9.9.9"}
	if got := bootstrapDNSFromEnv(); !reflect.DeepEqual(got, want) {
		t.Fatalf("bootstrap DNS = %#v, want %#v", got, want)
	}
}
