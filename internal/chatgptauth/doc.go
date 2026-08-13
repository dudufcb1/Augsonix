// Package chatgptauth resuelve credenciales de una suscripción ChatGPT para
// hablar con el backend de Codex (chatgpt.com/backend-api/codex/responses).
//
// Cubre tres cosas: obtener las credenciales por primera vez (Login con
// navegador o LoginDevice sin él), guardarlas en el estado privado de Reasonix,
// y entregar un bearer vigente en cada request, refrescándolo solo cuando le
// quedan pocos minutos de vida.
//
// Nada aquí depende del resto del kernel: solo net/http y el sistema de
// archivos. Eso lo hace importable desde internal/provider sin cruzar capas.
//
// El client_id es el público del Codex CLI y el redirect está registrado en el
// puerto 1455: ninguno de los dos es configurable porque cambiarlos hace que
// auth.openai.com rechace la autorización.
package chatgptauth
