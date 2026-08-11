package cli

import (
	"fmt"
	"strconv"

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

// indexStatusBody redacta el avance. Con el índice al día muestra cuántos
// chunks tiene: sin ese número no habría forma de distinguir "indexado" de "no
// configurado", que se veían igual —vacíos— y dejaban al usuario adivinando.
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
	case "ready":
		if st.Chunks == 0 {
			return ""
		}
		return footerValue(strconv.Itoa(st.Chunks))
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
