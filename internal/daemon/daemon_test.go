package daemon

import (
	"reflect"
	"testing"
)

func TestChildArgsRemovesDaemonFlagAndAddsInternalMarker(t *testing.T) {
	got := ChildArgs([]string{"start", "--d", "--s", "127.0.0.1", "--p", "7433"})
	want := []string{"start", "--s", "127.0.0.1", "--p", "7433", "--internal-daemon"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChildArgs() = %#v, want %#v", got, want)
	}
}
