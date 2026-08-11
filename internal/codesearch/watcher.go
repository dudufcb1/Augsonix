package codesearch

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// defaultPollInterval es cada cuánto se mira el árbol por si cambió desde
	// fuera. Es la red de seguridad, no la vía principal: lo que edita el propio
	// agente entra por Notify y no espera al reloj.
	defaultPollInterval = 5 * time.Second
	// defaultQuietPeriod es cuánto tiene que estar quieto el árbol antes de
	// reindexar. Sin esta espera, un git checkout que toca cientos de archivos
	// dispararía un reindexado por cada pasada del reloj.
	defaultQuietPeriod = 2 * time.Second
)

// Watcher mantiene el índice al día por dos vías: Notify avisa al instante de
// una escritura conocida, y el sondeo cubre lo que venga de fuera. Sondear en
// vez de suscribirse a eventos del sistema de archivos evita una dependencia,
// no topa con el límite de watches del kernel —que un repositorio grande
// agota— y es el mismo código en Linux, macOS y Windows.
type Watcher struct {
	Index    *Index
	Interval time.Duration
	Quiet    time.Duration

	once sync.Once
	wake chan struct{}
}

// Notify pide una revisión inmediata. No bloquea ni acumula: varias escrituras
// seguidas valen por una, y el periodo de calma decide cuándo indexar.
func (w *Watcher) Notify() {
	w.ensure()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Watcher) ensure() {
	w.once.Do(func() { w.wake = make(chan struct{}, 1) })
}

// Run sondea hasta que se cancele el contexto. Bloquea, así que va en su propia
// goroutine.
func (w *Watcher) Run(ctx context.Context) {
	if w.Index == nil {
		return
	}
	interval, quiet := w.Interval, w.Quiet
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if quiet <= 0 {
		quiet = defaultQuietPeriod
	}

	w.ensure()
	t := time.NewTicker(interval)
	defer t.Stop()

	// indexed arranca en cero a propósito: dar por indexado lo que hay en disco
	// dejaría invisible cualquier cambio ocurrido antes de esta foto. La primera
	// pasada verifica, y como Sync es incremental no cuesta nada si ya está al día.
	var indexed [32]byte
	seen := w.fingerprint(ctx)
	since := time.Now()
	// settle se reprograma con cada cambio. Sin él, un aviso por Notify detecta
	// el cambio y luego nadie vuelve a mirar para confirmar que ya se calmó.
	settle := time.After(quiet)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.wake:
		case <-settle:
			settle = nil
		}
		current := w.fingerprint(ctx)
		if current != seen {
			// Sigue moviéndose: reinicia la espera en vez de indexar a media
			// escritura y tener que rehacerlo enseguida.
			seen, since = current, time.Now()
			settle = time.After(quiet)
			continue
		}
		if current == indexed || time.Since(since) < quiet {
			continue
		}
		if _, err := w.Index.Sync(ctx, nil); err == nil {
			indexed = current
		}
	}
}

// fingerprint resume el árbol por ruta, tamaño y fecha de modificación. No lee
// contenido: decidir "algo cambió" con stat cuesta milisegundos, y confirmar qué
// cambió exactamente ya lo hace Sync con los hashes.
func (w *Watcher) fingerprint(ctx context.Context) [32]byte {
	files, err := w.Index.collect(ctx)
	if err != nil {
		return [32]byte{}
	}
	h := sha256.New()
	buf := make([]byte, 8)
	for _, rel := range files {
		info, err := os.Stat(filepath.Join(w.Index.root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		h.Write([]byte(rel))
		binary.LittleEndian.PutUint64(buf, uint64(info.Size()))
		h.Write(buf)
		binary.LittleEndian.PutUint64(buf, uint64(info.ModTime().UnixNano()))
		h.Write(buf)
	}
	return [32]byte(h.Sum(nil))
}
