package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"reasonix/internal/chatgptauth"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
)

// authUsageLine es la ayuda del comando; vive aparte para que una prueba pueda
// comprobar que los subcomandos siguen anunciándose.
const authUsageLine = "usage: reasonix auth <login [--headless]|status|logout> [openai]"

// authCommand administra la sesión de una suscripción, que no es una llave de
// API y por eso no cabe en `reasonix setup`: se obtiene autorizando en el
// navegador y se refresca sola mientras se usa.
func authCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, authUsageLine)
		return 2
	}
	rest := args[1:]
	if provider := authProviderArg(rest); provider != "" && provider != "openai" && provider != "chatgpt" {
		fmt.Fprintf(os.Stderr, "auth: no conozco el proveedor %q; hoy solo existe openai\n", provider)
		return 2
	}
	switch args[0] {
	case "login":
		return authLogin(hasFlag(rest, "--headless"))
	case "status":
		return authStatus()
	case "logout":
		return authLogout()
	default:
		fmt.Fprintf(os.Stderr, "auth: unknown operation %q\n%s\n", args[0], authUsageLine)
		return 2
	}
}

// authProviderArg devuelve el proveedor nombrado en la línea, si lo hay. El
// argumento es opcional mientras solo exista uno.
func authProviderArg(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return strings.ToLower(strings.TrimSpace(arg))
		}
	}
	return ""
}

func authLogin(headless bool) int {
	store, client, err := authContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), chatgptauth.LoginTimeout)
	defer cancel()

	var tokens chatgptauth.Tokens
	if headless {
		tokens, err = authLoginHeadless(ctx, store, client)
	} else {
		fmt.Println("Abriendo el navegador para autorizar con ChatGPT…")
		tokens, err = chatgptauth.Login(ctx, store, client, openAuthorizationURL)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		return 1
	}
	fmt.Printf("Sesión guardada en %s\n", store.Path)
	printPlanNotice(tokens)
	return 0
}

func authLoginHeadless(ctx context.Context, store chatgptauth.Store, client *http.Client) (chatgptauth.Tokens, error) {
	device, err := chatgptauth.StartDeviceLogin(ctx, client)
	if err != nil {
		return chatgptauth.Tokens{}, err
	}
	fmt.Printf("Abre %s en cualquier dispositivo y teclea el código: %s\n", device.URL, device.UserCode)
	fmt.Println("Esperando la autorización…")
	return chatgptauth.WaitDeviceLogin(ctx, store, client, device)
}

func authStatus() int {
	store, _, err := authContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		return 1
	}
	tokens, err := store.Load()
	if errors.Is(err, chatgptauth.ErrNoCredentials) {
		fmt.Println("openai: sin sesión. Corre `reasonix auth login openai`.")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		return 1
	}
	fmt.Printf("openai: sesión activa (cuenta %s)\n", orUnknown(tokens.AccountID))
	if expiry := chatgptauth.ExpiresAt(tokens.AccessToken); !expiry.IsZero() {
		fmt.Printf("  el token vence %s (se refresca solo)\n", expiry.Local().Format(time.RFC1123))
	}
	printPlanNotice(tokens)
	return 0
}

func authLogout() int {
	store, _, err := authContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		return 1
	}
	if err := store.Clear(); err != nil {
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		return 1
	}
	fmt.Println("Sesión de ChatGPT borrada.")
	return 0
}

// printPlanNotice avisa cuando el plan de la cuenta no alcanza. El backend de
// Codex rechaza todos los modelos con un 400 genérico, y sin este aviso el
// usuario lo lee como si el modelo estuviera mal escrito.
func printPlanNotice(tokens chatgptauth.Tokens) {
	plan := chatgptauth.PlanType(tokens)
	if plan == "" {
		return
	}
	fmt.Printf("  plan: %s\n", plan)
	if strings.EqualFold(plan, "free") {
		fmt.Println("  aviso: el backend de Codex solo atiende cuentas Plus, Pro o Business.")
	}
}

func authContext() (chatgptauth.Store, *http.Client, error) {
	store := chatgptauth.Store{Path: config.ChatGPTAuthPath(), Fallback: config.CodexCLIAuthPath()}
	if strings.TrimSpace(store.Path) == "" {
		return store, nil, fmt.Errorf("no se pudo resolver el directorio de estado de Reasonix")
	}
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	proxy := netclient.ProxySpec{Mode: netclient.ModeAuto}
	if cfg, err := config.LoadForRootReadOnly(root); err == nil {
		proxy = cfg.NetworkProxySpec()
	}
	client, err := netclient.NewHTTPClient(proxy, netclient.TransportOptions{})
	if err != nil {
		return store, nil, err
	}
	return store, client, nil
}

func openAuthorizationURL(rawURL string) error {
	cmd, err := mcpOpenCommand(rawURL)
	if err != nil {
		return err
	}
	return cmd.Start()
}

func orUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "desconocida"
	}
	return value
}
