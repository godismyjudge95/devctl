package services

import (
	"testing"
	"time"
)

func TestCurrentStates_BeforeFirstPollMatchesGetStateInstalled(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(Definition{ID: "php-fpm-7.0", Label: "PHP 7.0 FPM"})
	reg.Register(Definition{ID: "mailpit", Label: "Mailpit", Installable: true})
	p := NewPoller(reg, NewManager(nil, &Supervisor{procs: map[string]*managedProc{}}), time.Hour)

	got := map[string]bool{}
	for _, st := range p.CurrentStates() {
		got[st.ID] = st.Installed
	}
	if !got["php-fpm-7.0"] {
		t.Error("php-fpm-7.0 installed=false before first poll, want true")
	}
	if got["mailpit"] {
		t.Error("mailpit installed=true before first poll, want false")
	}
}
