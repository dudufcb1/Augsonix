package cli

import (
	"fmt"

	"reasonix/internal/control"
	"reasonix/internal/i18n"
)

// indexStatusGroup describe el avance del índice semántico para el pie de
// pantalla, o "" cuando no hay nada que decir. Un índice al día no se anuncia:
// el indicador existe para explicar una espera, no para ocupar espacio.
func (m chatTUI) indexStatusGroup() string {
	if m.ctrl == nil {
		return ""
	}
	st, ok := m.ctrl.IndexStatus()
	if !ok {
		return ""
	}
	body := indexStatusBody(st)
	if body == "" {
		return ""
	}
	return footerMetric(i18n.M.ChatStatusIndexLabel, body)
}

// indexStatusBody redacta el avance. El primer indexado de un proyecto tarda y
// se anuncia como tal; los siguientes solo se ven si de verdad hay que embeber
// algo, para que abrir una sesión con el índice al día no parpadee.
func indexStatusBody(st control.IndexStatus) string {
	switch st.Phase {
	case "quota":
		return themeFg(activeCLITheme.danger, i18n.M.ChatStatusIndexQuota)
	case "failed":
		return themeFg(activeCLITheme.danger, i18n.M.ChatStatusIndexFailed)
	case "scanning":
		if st.First {
			return footerInfo(i18n.M.ChatStatusIndexStarting)
		}
		return ""
	case "indexing":
		return footerInfo(indexProgressText(st))
	default:
		return ""
	}
}

// indexProgressText arma "123/450 (27%)". El porcentaje va aparte del conteo
// porque a simple vista dice si conviene esperar o seguir trabajando.
func indexProgressText(st control.IndexStatus) string {
	if st.Total <= 0 {
		return fmt.Sprintf("%d", st.Done)
	}
	pct := st.Done * 100 / st.Total
	return fmt.Sprintf("%d/%d (%d%%)", st.Done, st.Total, pct)
}
