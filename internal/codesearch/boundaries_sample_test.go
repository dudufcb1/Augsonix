package codesearch

// Muestras de código usadas por las pruebas del troceo. Van como constantes y
// no como archivos en testdata porque el contenido exacto —dónde cae cada
// línea— es parte de lo que se verifica.

const goSample = `package sample

import "strings"

// Normalize deja el texto listo para comparar.
func Normalize(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// Counter lleva la cuenta de ocurrencias.
type Counter struct {
	total int
}

func (c *Counter) Add(n int) {
	c.total += n
}

func (c *Counter) Total() int {
	return c.total
}
`

const pythonSample = `import calendar
from decimal import Decimal


class WalletBalances(dict):
    """Saldos de la bolsa del periodo."""

    included = 0
    extra = 0


def money(value: object) -> Decimal:
    """Convierte cualquier numero a Decimal con dos posiciones."""
    return Decimal(str(value)).quantize(Decimal("0.01"))


def quote_plan(
    operations: int,
    factor: Decimal,
) -> Decimal:
    base = money(operations) * factor
    return base


@staticmethod
def decorada(valor):
    """Lleva decorador arriba y debe viajar con el."""
    return valor * 2
`

const phpSample = `<?php

namespace App\Services;

/**
 * Registra los fallos de los servicios externos.
 */
class FailureRecorder
{
    public function record(string $service, string $reason): void
    {
        $this->store->put($service, $reason);
    }

    public function clear(): void
    {
        $this->store->flush();
    }
}

function normalizeAccount(?string $number): string
{
    return trim((string) $number);
}
`

const tsSample = `import type { Context } from "./types";

export async function searchCode(
  args: { query: string; limit?: number },
  ctx: Context
): Promise<string> {
  const found = await ctx.index.search(args.query);
  return found.join("\n");
}

export class Registry {
  private items: string[] = [];

  add(item: string): void {
    this.items.push(item);
  }

  all(): string[] {
    return this.items;
  }
}
`

// hostilePython lleva parentesis y llaves dentro de cadenas y comentarios: sin
// limpiarlas antes de contar, el archivo entero se lee como una sola funcion.
const hostilePython = `def primera(nombre):
    """Formato: {nombre} y un parentesis suelto ( ojo."""
    print("(")
    print("algo)")
    return f"hola {nombre}"


def segunda(a, b):
    # comentario con parentesis abierto ( y llave {
    resultado = a + b
    return resultado


class Tercera:
    def metodo(self):
        texto = "cadena con def falsa( dentro"
        return texto

    def otro(self):
        return 42
`

// hostileJS lleva llaves dentro de expresiones regulares, que un contador de
// llaves confunde con el cierre del cuerpo de la funcion.
const hostileJS = `function primera(texto) {
  const cierre = /\}/;
  const apertura = /[{]/;
  return texto.replace(cierre, "");
}

function segunda(a) {
  const division = 10 / a / 2;
  return division;
}

function tercera() {
  return "ok";
}
`

// hostilePHP lleva un heredoc con llaves desbalanceadas y comentarios con
// almohadilla, que abren un comentario en PHP pero no en la familia C.
const hostilePHP = `<?php

function conHeredoc(string $nombre): string
{
    $texto = <<<EOT
    Hola $nombre, aqui va una llave { sin cerrar
    y otra } suelta
    EOT;
    return $texto;
}

function despues(): int
{
    # comentario con llave { abierta
    return 1;
}
`
