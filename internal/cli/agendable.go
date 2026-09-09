package cli

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/schedulable"
)

// runAgendableCommand handles "/agendable": with no argument it authorizes the
// scheduler to wake this session, "off" revokes it, and "list" shows what is
// authorized. The mark never expires; only the user removes it.
func (m *chatTUI) runAgendableCommand(input string) {
	m.echoLocalCommand(input)
	args := tokenizeArgs(input)
	sub := ""
	if len(args) > 1 {
		sub = strings.ToLower(strings.TrimSpace(args[1]))
	}
	reg, err := schedulable.Open(config.SchedulableDir())
	if err != nil {
		m.notice("agendable: " + err.Error())
		return
	}
	switch sub {
	case "", "on":
		m.markSessionSchedulable(reg)
	case "off":
		m.unmarkSessionSchedulable(reg)
	case "list":
		m.listSchedulableSessions(reg)
	default:
		m.notice("usage: /agendable [off|list]")
	}
}

// markSessionSchedulable persiste la sesión antes de autorizarla: el
// planificador reanuda desde el archivo, así que una sesión que todavía no
// existe en disco no habría cómo despertarla.
func (m *chatTUI) markSessionSchedulable(reg *schedulable.Registry) {
	if ensurer, ok := m.ctrl.(interface{ EnsureSessionPath() }); ok {
		ensurer.EnsureSessionPath()
	}
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("agendable: could not save the current session: " + err.Error())
		return
	}
	path := m.ctrl.SessionPath()
	if strings.TrimSpace(path) == "" {
		m.notice("agendable: this session has no file yet; send a message first")
		return
	}
	entry, err := reg.Mark(context.Background(), schedulable.Entry{
		SessionID:   agent.BranchID(path),
		SessionPath: path,
		WorkDir:     m.ctrl.WorkspaceRoot(),
	})
	if err != nil {
		m.notice("agendable: " + err.Error())
		return
	}
	m.notice(fmt.Sprintf("agendable: the scheduler may wake this session (%s) — /agendable off revokes it", entry.SessionID))
}

func (m *chatTUI) unmarkSessionSchedulable(reg *schedulable.Registry) {
	path := m.ctrl.SessionPath()
	if strings.TrimSpace(path) == "" {
		m.notice("agendable: this session has no file yet")
		return
	}
	if err := reg.Unmark(context.Background(), agent.BranchID(path)); err != nil {
		m.notice("agendable: " + err.Error())
		return
	}
	m.notice("agendable: revoked; the scheduler can no longer wake this session")
}

func (m *chatTUI) listSchedulableSessions(reg *schedulable.Registry) {
	entries, err := reg.List()
	if err != nil {
		m.notice("agendable: " + err.Error())
		return
	}
	if len(entries) == 0 {
		m.notice("agendable: no session is authorized yet")
		return
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("agendable: %d authorized session(s)", len(entries)))
	current := agent.BranchID(m.ctrl.SessionPath())
	for _, e := range entries {
		mark := ""
		if e.SessionID == current {
			mark = " (current)"
		}
		b.WriteString(fmt.Sprintf("\n  %s  %s  %s%s",
			e.MarkedAt.Local().Format("2006-01-02 15:04"), e.SessionID, e.WorkDir, mark))
	}
	m.notice(b.String())
}
