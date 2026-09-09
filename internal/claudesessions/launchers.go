package claudesessions

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Launcher es un lanzador de escritorio del usuario. De ahí sale el nombre con
// el que él reconoce cada entorno: varios comparten home de configuración y
// solo se distinguen por la carpeta en que abren.
type Launcher struct {
	Name string
	Home string
	Dir  string
}

var (
	reConfigHome = regexp.MustCompile(`CLAUDE_CONFIG_DIR=([^\s;'"]+)`)
	reWorkingDir = regexp.MustCompile(`--working-directory=([^\s;'"]+)`)
	// reNameNoise recorta lo que se repite en todos los lanzadores y no
	// distingue a ninguno, para que el chip quepa en una celda.
	reNameNoise = regexp.MustCompile(`(?i)\s*(claude|code|resume|\(proxy\))\s*`)
)

// LoadLaunchers lee los .desktop de un directorio. Los que no nombran ni un
// home ni una carpeta se omiten: no identifican ningún entorno.
func LoadLaunchers(dir, defaultHome string) []Launcher {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Launcher
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".desktop") {
			continue
		}
		l, ok := parseLauncher(filepath.Join(dir, e.Name()), defaultHome)
		if ok {
			out = append(out, l)
		}
	}
	return out
}

func parseLauncher(path, defaultHome string) (Launcher, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Launcher{}, false
	}
	defer f.Close()
	var l Launcher
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Name="):
			l.Name = cleanLauncherName(strings.TrimPrefix(line, "Name="))
		case strings.HasPrefix(line, "Exec="):
			if !strings.Contains(line, "claude") {
				return Launcher{}, false
			}
			if m := reConfigHome.FindStringSubmatch(line); len(m) == 2 {
				l.Home = strings.TrimRight(m[1], "/;")
			}
			if m := reWorkingDir.FindStringSubmatch(line); len(m) == 2 {
				l.Dir = strings.TrimRight(m[1], "/")
			}
		}
	}
	if l.Name == "" || (l.Home == "" && l.Dir == "") {
		return Launcher{}, false
	}
	if l.Home == "" {
		l.Home = defaultHome
	}
	return l, true
}

// cleanLauncherName quita las palabras que comparten todos los lanzadores.
func cleanLauncherName(raw string) string {
	name := strings.TrimSpace(reNameNoise.ReplaceAllString(strings.TrimSpace(raw), " "))
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return strings.TrimSpace(raw)
	}
	return name
}

// LabelFor elige el nombre del entorno para una sesión. Home y carpeta juntos
// mandan, porque es lo que de verdad distingue un lanzador de otro; la carpeta
// sola sirve cuando la sesión se abrió a mano desde ahí.
func LabelFor(launchers []Launcher, home, cwd string) string {
	home, cwd = strings.TrimRight(home, "/"), strings.TrimRight(cwd, "/")
	var byDir string
	for _, l := range launchers {
		if l.Home == home && l.Dir != "" && l.Dir == cwd {
			return l.Name
		}
		if byDir == "" && l.Dir != "" && l.Dir == cwd {
			byDir = l.Name
		}
	}
	return byDir
}

// ApplyLaunchers etiqueta cada sesión con el entorno del que salió, dejando el
// nombre del home cuando ningún lanzador coincide.
func ApplyLaunchers(sessions []Session, launchers []Launcher) []Session {
	for i := range sessions {
		if name := LabelFor(launchers, sessions[i].Home, sessions[i].Cwd); name != "" {
			sessions[i].Launcher = name
		}
	}
	return sessions
}
