package codesearch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newGitRepo arma un repositorio temporal con los commits que se le pidan, para
// probar la extracción contra git de verdad y no contra una simulación.
func newGitRepo(t *testing.T, commits ...[2]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está disponible")
	}
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "prueba@ejemplo.local"},
		{"config", "user.name", "Prueba"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	for i, c := range commits {
		name := filepath.Join(root, "archivo.txt")
		if err := os.WriteFile(name, []byte(strings.Repeat("linea\n", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", c[0], "-m", c[1]}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = root
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
			}
		}
	}
	return root
}

func TestExtractCommitsReadsTheHistory(t *testing.T) {
	// La extracción tiene que traer asunto, cuerpo y archivos tocados: es todo
	// lo que se indexa, porque no hay ningún modelo generando descripciones.
	root := newGitRepo(t,
		[2]string{"primer cambio", "explica por que se hizo"},
		[2]string{"segundo cambio", "con su propio motivo"},
	)
	got, err := ExtractCommits(context.Background(), root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("se extrajeron %d commits, esperaba 2", len(got))
	}
	// git log devuelve el más reciente primero.
	if got[0].Subject != "segundo cambio" {
		t.Errorf("asunto del primero = %q", got[0].Subject)
	}
	if !strings.Contains(got[0].Body, "su propio motivo") {
		t.Errorf("no se recuperó el cuerpo: %q", got[0].Body)
	}
	if len(got[0].Files) != 1 || got[0].Files[0] != "archivo.txt" {
		t.Errorf("archivos tocados = %v", got[0].Files)
	}
	if got[0].Date == "" || got[0].Author == "" {
		t.Errorf("falta fecha o autor: %q %q", got[0].Date, got[0].Author)
	}
}

func TestExtractCommitsHonorsTheLimit(t *testing.T) {
	// La historia vieja se busca poco y cada commit cuesta cuota, así que el
	// límite tiene que respetarse de verdad.
	root := newGitRepo(t,
		[2]string{"uno", ""}, [2]string{"dos", ""}, [2]string{"tres", ""},
	)
	got, err := ExtractCommits(context.Background(), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("se extrajeron %d commits con límite 2", len(got))
	}
}

func TestExtractCommitsToleratesADirectoryWithoutGit(t *testing.T) {
	// Abrir una carpeta que no es repositorio es normal. No debe ser un error:
	// simplemente no hay historia que ofrecer.
	got, err := ExtractCommits(context.Background(), t.TempDir(), 10)
	if err != nil {
		t.Fatalf("devolvió error en vez de una historia vacía: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("devolvió %d commits fuera de un repositorio", len(got))
	}
}

func TestCommitDocumentLeadsWithTheMessage(t *testing.T) {
	// El mensaje va primero porque es lo que dice la intención del cambio, que
	// es lo que alguien busca. El diff va al final, como contexto técnico.
	c := Commit{
		Hash: "abcdef1234", Date: "2026-08-11", Author: "Alguien",
		Subject: "arregla el cobro duplicado",
		Body:    "la pasarela reintentaba el aviso",
		Files:   []string{"app/Billing.php"},
		Diff:    "@@ -1 +1 @@\n-viejo\n+nuevo\n",
	}
	doc := c.Document()
	if !strings.HasPrefix(doc, "arregla el cobro duplicado\n") {
		t.Errorf("el documento no arranca con el asunto: %q", doc[:40])
	}
	for _, want := range []string{"la pasarela reintentaba", "app/Billing.php", "2026-08-11", "+nuevo"} {
		if !strings.Contains(doc, want) {
			t.Errorf("el documento no incluye %q", want)
		}
	}
	if idx := strings.Index(doc, "@@"); idx >= 0 && idx < strings.Index(doc, "app/Billing.php") {
		t.Error("el diff quedó antes de los archivos y el mensaje")
	}
}

func TestCommitSummaryIsOneReadableLine(t *testing.T) {
	// El resumen es lo que ve el agente en la lista de resultados: hash corto,
	// fecha y asunto, sin saltos de línea que rompan el formato.
	c := Commit{Hash: "abcdef1234567890", Date: "2026-08-11", Subject: "algo"}
	got := c.Summary()
	if strings.Contains(got, "\n") {
		t.Errorf("el resumen trae saltos de línea: %q", got)
	}
	if !strings.HasPrefix(got, "abcdef12 ") {
		t.Errorf("el resumen no arranca con el hash corto: %q", got)
	}
}
