package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/ThowiLabs/tavito-guard/internal/guard"
	"github.com/YahirHub/svcforge"
	"os"
	"strings"
	"time"
)

func main() {
	marker, _ := os.ReadFile("/.tavito-openrc-fixture")
	if os.Getenv("TAVITO_OPENRC_QA") != "1" || string(marker) != "TAVITO-OPENRC-ISOLATED-QA" {
		fmt.Fprintln(os.Stderr, "Fixture: requiere chroot aislado preparado para esta prueba")
		os.Exit(2)
	}
	operation := flag.String("operation", "install", "operación de prueba")
	bad := flag.Bool("reproduce", false, "reproducir política anterior")
	flag.Parse()
	c := guard.DefaultConfig()
	c.Token = "123456789:fixture_sin_token_real"
	c.Username = "fixture"
	path := "/etc/tavito-guard/config.json"
	var err error
	if _, err = os.Stat(path); os.IsNotExist(err) {
		c.Password, err = guard.NewPassword("clave-exclusiva-de-fixture")
		if err != nil {
			panic(err)
		}
		if err = guard.SaveConfig(path, c); err != nil {
			panic(err)
		}
	} else {
		c, err = guard.LoadConfig(path)
		if err != nil {
			panic(err)
		}
	}
	if _, err = guard.OpenStore(c.DataDir); err != nil {
		panic(err)
	}
	if err = guard.AtomicJSON(c.DataDir+"/heartbeat.json", map[string]any{"version": "0.1.1", "pid": os.Getpid(), "time": time.Now()}); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if *bad {
		app := guard.AppSpec(c, path, "0.1.1")
		app.Service.Restart = svcforge.RestartOnFailure
		_, err = (svcforge.Runner{App: app}).Execute(ctx, svcforge.OperationInstall)
		if err == nil || !strings.Contains(err.Error(), "OpenRC restart policy") {
			panic(fmt.Sprintf("no se reprodujo el error: %v", err))
		}
		fmt.Println("REPRODUCIDO:", err)
		return
	}
	result, err := guard.Lifecycle(ctx, c, path, "0.1.1", svcforge.Operation(*operation))
	if err != nil {
		panic(err)
	}
	fmt.Printf("CORRECTO: %s; gestor=%s; instalado=%v; activo=%v\n", result.Operation, result.ServiceManager, result.Installed, result.ServiceRunning)
}
