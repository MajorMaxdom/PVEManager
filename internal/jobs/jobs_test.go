package jobs

import (
	"errors"
	"testing"
)

func TestSingleRunningJobPerServerAndLogOffsets(t *testing.T) {
	r := New()
	j, ok := r.Start("pve1", "upgrade", "u")
	if !ok {
		t.Fatal("first start refused")
	}
	if j2, ok := r.Start("pve1", "upgrade", "u"); ok || j2.ID != j.ID {
		t.Fatal("second concurrent job must return the running one")
	}
	if _, ok := r.Start("pve2", "upgrade", "u"); !ok {
		t.Fatal("other server must be allowed")
	}
	for _, l := range []string{"a", "b", "c"} {
		j.Append(l)
	}
	v := j.View(1)
	if len(v.Lines) != 2 || v.Lines[0] != "b" || v.Next != 3 {
		t.Fatalf("bad view %+v", v)
	}
	j.Finish(errors.New("boom"))
	if v := j.View(3); v.Status != "failed" || v.Error != "boom" || len(v.Lines) != 0 {
		t.Fatalf("bad final view %+v", v)
	}
	if _, ok := r.Start("pve1", "upgrade", "u"); !ok {
		t.Fatal("finished job must not block a new one")
	}
}

func TestLogTrim(t *testing.T) {
	j := &Job{}
	for i := 0; i < maxLines+10; i++ {
		j.Append("x")
	}
	if v := j.View(0); len(v.Lines) != maxLines || v.Next != maxLines+10 {
		t.Fatalf("trim wrong: %d %d", len(v.Lines), v.Next)
	}
}
