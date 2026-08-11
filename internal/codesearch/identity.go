package codesearch

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// IDSource dice de dónde se dedujo la identidad del workspace, para poder
// explicarle al usuario por qué su índice se reusó o no.
type IDSource string

const (
	// SourceRemote es el caso bueno: el mismo repositorio en cualquier máquina.
	SourceRemote IDSource = "git remote"
	// SourcePath identifica un proyecto local; no sobrevive a mover la carpeta,
	// pero sin remoto tampoco hay un índice remoto que reencontrar.
	SourcePath IDSource = "workspace path"
)

// Workspace identifica el proyecto que se está indexando.
type Workspace struct {
	// ID es estable entre máquinas y clones del mismo repositorio, para que el
	// índice remoto se reencuentre sin que el usuario configure nada.
	ID string
	// Name es la carpeta base, solo para mostrar.
	Name   string
	Source IDSource
}

// IdentifyWorkspace deduce la identidad de root: el remoto de git si lo hay,
// porque es lo único que sobrevive a clonar en otra máquina, y si no la ruta.
// No se usa el commit raíz: leerlo de verdad exige recorrer el historial, y el
// commit de HEAD cambiaría la identidad en cada commit nuevo.
func IdentifyWorkspace(root string) Workspace {
	return IdentifyWorkspaceIn(root, nil)
}

// IdentifyWorkspaceIn hace lo mismo sabiendo qué carpetas agrupan proyectos. En
// un repositorio normal un subdirectorio comparte identidad con la raíz, porque
// quien abre internal/agent quiere el índice del proyecto. Cuando el
// repositorio ES un contenedor, cada subcarpeta va aparte: compartir índice
// haría que la segunda borrara los archivos de la primera.
func IdentifyWorkspaceIn(root string, containers []string) Workspace {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = filepath.Clean(root)
	}
	name := filepath.Base(abs)

	if gitDir := findGitDir(abs); gitDir != "" {
		if remote := originURL(gitDir); remote != "" {
			key := "remote:" + normalizeRemote(remote)
			if isWithinContainer(filepath.Dir(gitDir), containers) {
				if sub := subPathWithin(gitDir, abs); sub != "" {
					key += "#" + sub
				}
			}
			return Workspace{ID: hashID(key), Name: name, Source: SourceRemote}
		}
	}
	return Workspace{ID: hashID("path:" + abs), Name: name, Source: SourcePath}
}

// isWithinContainer reporta si repoRoot es una de las carpetas contenedoras.
func isWithinContainer(repoRoot string, containers []string) bool {
	target := filepath.Clean(repoRoot)
	for _, c := range containers {
		if c == "" {
			continue
		}
		if abs, err := filepath.Abs(c); err == nil && filepath.Clean(abs) == target {
			return true
		}
	}
	return false
}

// subPathWithin devuelve la ruta de abs relativa a la raíz del repositorio, con
// separadores "/", o cadena vacía si abs es esa misma raíz. gitDir apunta al
// directorio .git, así que la raíz es su carpeta contenedora.
func subPathWithin(gitDir, abs string) string {
	repoRoot := filepath.Dir(gitDir)
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// normalizeRemote lleva las formas de una misma URL a un texto común, para que
// clonar por SSH o por HTTPS no produzca dos índices distintos.
func normalizeRemote(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	if i := strings.Index(s, "://"); i != -1 {
		s = s[i+3:]
	}
	s = strings.TrimPrefix(s, "git@")
	// scp-like: "github.com:owner/repo" pasa a "github.com/owner/repo".
	if at := strings.LastIndex(s, "@"); at != -1 {
		s = s[at+1:] // descarta credenciales embebidas en la URL
	}
	s = strings.Replace(s, ":", "/", 1)
	return strings.ToLower(s)
}

func hashID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:8])
}

// findGitDir devuelve el directorio .git de root, resolviendo el archivo .git
// que dejan los worktrees y submódulos. "" si root no está en un repositorio.
func findGitDir(root string) string {
	for dir := root; ; {
		candidate := filepath.Join(dir, ".git")
		if info, err := os.Stat(candidate); err == nil {
			if info.IsDir() {
				return candidate
			}
			if target := gitFileTarget(candidate); target != "" {
				return target
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// gitFileTarget lee el "gitdir: <ruta>" de un .git que es archivo, como pasa en
// worktrees y submódulos.
func gitFileTarget(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}

// originURL saca la url del remoto origin de .git/config sin invocar al binario
// de git, que puede no estar instalado.
func originURL(gitDir string) string {
	f, err := os.Open(filepath.Join(gitDir, "config"))
	if err != nil {
		return ""
	}
	defer f.Close()

	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = strings.EqualFold(strings.TrimSpace(strings.Trim(line, "[]")), `remote "origin"`)
			continue
		}
		if !inOrigin {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(k), "url") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
