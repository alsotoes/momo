package metrics

import "testing"

// memModeStore is an in-memory ModeStore for resume/persist tests.
type memModeStore struct {
	mode    int
	present bool
	saved   []int
	loadErr error
	saveErr error
}

func (m *memModeStore) LoadMode() (int, bool, error) {
	return m.mode, m.present, m.loadErr
}

func (m *memModeStore) SaveMode(mode int) error {
	m.saved = append(m.saved, mode)
	return m.saveErr
}

func TestResumeModeIndex(t *testing.T) {
	order := []int{0, 2, 1}
	cases := map[int]int{0: 0, 2: 1, 1: 2, 99: 0}
	for mode, want := range cases {
		if got := resumeModeIndex(order, mode); got != want {
			t.Fatalf("mode %d: index %d, want %d", mode, got, want)
		}
	}
}

func TestResumeMode(t *testing.T) {
	order := []int{0, 2, 1}
	if got := resumeMode(order, nil); got != 0 {
		t.Fatalf("nil store must resume at 0, got %d", got)
	}
	if got := resumeMode(order, &memModeStore{}); got != 0 {
		t.Fatalf("absent value must resume at 0, got %d", got)
	}
	if got := resumeMode(order, &memModeStore{mode: 1, present: true}); got != 2 {
		t.Fatalf("persisted mode 1 must resume at index 2, got %d", got)
	}
	if got := resumeMode(order, &memModeStore{mode: 7, present: true}); got != 0 {
		t.Fatalf("stale mode must fall back to 0, got %d", got)
	}
}

func TestPersistMode(t *testing.T) {
	store := &memModeStore{}
	persistMode([]int{0, 2, 1}, 1, store)
	if len(store.saved) != 1 || store.saved[0] != 2 {
		t.Fatalf("saved %v, want [2]", store.saved)
	}
	// Errors are audited, not fatal.
	persistMode([]int{0}, 0, &memModeStore{saveErr: errTest})
	persistMode([]int{0}, 0, nil) // nil store: no-op
}

// errTest is a sentinel for the persist-error path.
var errTest = &testErr{}

type testErr struct{}

func (*testErr) Error() string { return "boom" }
