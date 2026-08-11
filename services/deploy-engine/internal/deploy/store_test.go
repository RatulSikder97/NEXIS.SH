package deploy

import (
	"sync"
	"testing"
)

func TestStore_PutGet(t *testing.T) {
	s := NewStore()
	if _, ok := s.Get("missing"); ok {
		t.Fatal("empty store must miss")
	}
	s.Put("d1", Record{ContainerName: "nexis-preview-d1", Result: Result{DeploymentID: "d1", Status: StatusRunning}})
	rec, ok := s.Get("d1")
	if !ok || rec.Result.Status != StatusRunning || rec.ContainerName != "nexis-preview-d1" {
		t.Fatalf("unexpected record: %+v ok=%v", rec, ok)
	}
}

func TestStore_GetReturnsCopy(t *testing.T) {
	s := NewStore()
	s.Put("d1", Record{Result: Result{Status: StatusRunning}})
	rec, _ := s.Get("d1")
	rec.Result.Status = "mutated"
	again, _ := s.Get("d1")
	if again.Result.Status != StatusRunning {
		t.Fatal("Get must return a copy, not a shared pointer view")
	}
}

func TestStore_SetStatus(t *testing.T) {
	s := NewStore()
	s.SetStatus("missing", StatusStopped) // must not panic
	s.Put("d1", Record{Result: Result{Status: StatusRunning}})
	s.SetStatus("d1", StatusStopped)
	rec, _ := s.Get("d1")
	if rec.Result.Status != StatusStopped {
		t.Fatalf("status = %q, want stopped", rec.Result.Status)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.Put("d", Record{Result: Result{Status: StatusRunning}})
		}()
		go func() {
			defer wg.Done()
			s.Get("d")
			s.SetStatus("d", StatusStopped)
		}()
	}
	wg.Wait()
}

func TestContainerName(t *testing.T) {
	if got := ContainerName("123e4567-e89b-42d3-a456-426614174000"); got != "nexis-preview-123e4567" {
		t.Fatalf("ContainerName = %q", got)
	}
	if got := ContainerName("short"); got != "nexis-preview-short" {
		t.Fatalf("ContainerName = %q", got)
	}
}
