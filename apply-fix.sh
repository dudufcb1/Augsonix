#!/usr/bin/env bash
set -euo pipefail

# Eduardo custom patches para reasonix.
# Aplica todos los .patch de patches/, compila y (opcional) instala el binario.
#
#   ./apply-fix.sh             # aplica parches + compila -> bin/reasonix
#   ./apply-fix.sh --install   # ademas reemplaza el binario instalado (sudo, backup)
#   ./apply-fix.sh --check     # solo verifica que los parches aplican limpio
#
# Actualizar upstream antes de aplicar:
#   git fetch origin && git reset --hard origin/main-v2

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MODE="${1:-build}"
INSTALL_BIN="/usr/local/lib/node_modules/reasonix/node_modules/@reasonix/cli-linux-x64/bin/reasonix"

# sudo_run: usa "sudo -S" con la contraseña de SUDO_PASSWORD cuando existe
# (entornos no interactivos); si no, sudo normal pide la contraseña en la TTY.
sudo_run() {
  if [ -n "${SUDO_PASSWORD:-}" ]; then
    printf '%s\n' "$SUDO_PASSWORD" | sudo -S "$@"
  else
    sudo "$@"
  fi
}

echo "==> Reasonix patches"

if [ "$MODE" = "--check" ]; then
  APPLY=check
elif [ "$MODE" = "--install" ]; then
  APPLY=install
elif [ "$MODE" = "build" ]; then
  APPLY=build
else
  echo "uso: ./apply-fix.sh [build|--install|--check]" >&2
  exit 1
fi

echo "==> Aplicando patches..."
for PATCH in "$SCRIPT_DIR"/patches/*.patch; do
  [ -f "$PATCH" ] || continue
  name="$(basename "$PATCH")"

  if git -C "$SCRIPT_DIR" apply --check "$PATCH" 2>/dev/null; then
    if [ "$APPLY" != "check" ]; then
      git -C "$SCRIPT_DIR" apply "$PATCH"
      echo "    $name -> aplicado limpio"
    else
      echo "    $name -> OK (aplicaria limpio)"
    fi
  elif git -C "$SCRIPT_DIR" apply --reverse --check "$PATCH" 2>/dev/null; then
    echo "    $name -> ya aplicado, skip"
  else
    if git -C "$SCRIPT_DIR" apply -C2 --check "$PATCH" 2>/dev/null; then
      if [ "$APPLY" != "check" ]; then
        git -C "$SCRIPT_DIR" apply -C2 "$PATCH"
        echo "    $name -> aplicado con fuzz (-C2)"
      else
        echo "    $name -> OK con fuzz (-C2)"
      fi
    else
      echo "    $name -> ERROR: no aplica" >&2
      exit 1
    fi
  fi
done

if [ "$APPLY" = "check" ]; then
  echo "==> Todos los patches aplicarian limpio."
  exit 0
fi

echo "==> Compilando..."
cd "$SCRIPT_DIR"
make build

if [ "$APPLY" = "install" ]; then
  if [ ! -f "$INSTALL_BIN" ]; then
    # Detectar el binario nativo del paquete npm en otro layout
    INSTALL_BIN="$(find /usr/local/lib/node_modules/reasonix -path '*/@reasonix/cli-*/bin/reasonix' -type f 2>/dev/null | head -1 || true)"
    if [ -z "${INSTALL_BIN:-}" ]; then
      echo "==> ERROR: no encuentro el binario instalado de reasonix" >&2
      exit 1
    fi
  fi
  echo "==> Instalando sobre $INSTALL_BIN (backup previo)"
  sudo_run cp "$INSTALL_BIN" "$INSTALL_BIN.pre-patch" 2>/dev/null || true
  cp "$SCRIPT_DIR/bin/reasonix" "$SCRIPT_DIR/bin/reasonix.new"
  sudo_run mv -f "$SCRIPT_DIR/bin/reasonix.new" "$INSTALL_BIN"
  sudo_run chmod 755 "$INSTALL_BIN"
  echo "==> Instalado. Version: $(reasonix version)"
fi

echo "==> Done. Binario: $SCRIPT_DIR/bin/reasonix"
