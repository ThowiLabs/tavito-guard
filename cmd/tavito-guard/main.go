package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/ThowiLabs/tavito-guard/internal/guard"
	"github.com/YahirHub/svcforge"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var version = "0.1.1"

func main() {
	if e := run(); e != nil && !errors.Is(e, context.Canceled) {
		fmt.Fprintln(os.Stderr, "Tavito Guard:", e)
		os.Exit(1)
	}
}
func run() error {
	install := flag.Bool("install", false, "Configurar e instalar Tavito Guard con svcforge")
	remove := flag.Bool("remove", false, "Desinstalar conservando configuración y datos")
	uninstall := flag.Bool("uninstall", false, "Alias de --remove")
	repair := flag.Bool("repair", false, "Reparar la instalación con svcforge")
	status := flag.Bool("status", false, "Mostrar estado de la instalación")
	serve := flag.Bool("serve", false, "Ejecutar el bot")
	setup := flag.Bool("setup", false, "Crear configuración sin instalar")
	apply := flag.Bool("apply-update", false, "Aplicar el artefacto de actualización verificado")
	showVersion := flag.Bool("version", false, "Mostrar versión")
	resetOwner := flag.Bool("reset-owner", false, "Revocar propietario y sesión desde la consola local")
	config := flag.String("config", "/etc/tavito-guard/config.json", "Archivo de configuración privado")
	dataDir := flag.String("data-dir", "", "Directorio de datos para la configuración inicial")
	render := flag.String("render", "", "Guardar una tarjeta PNG con recursos reales, sin iniciar el bot")
	flag.Parse()
	if *showVersion {
		fmt.Println("Tavito Guard", version)
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *render != "" {
		s, e := guard.Collect(ctx)
		if e != nil {
			return e
		}
		data, e := guard.ResourceCard(s)
		if e != nil {
			return e
		}
		return os.WriteFile(*render, data, 0600)
	}
	actions := 0
	for _, v := range []bool{*install, *remove || *uninstall, *repair, *status, *serve, *setup, *apply, *resetOwner} {
		if v {
			actions++
		}
	}
	if actions != 1 {
		flag.Usage()
		return errors.New("selecciona exactamente una operación")
	}
	path, e := filepath.Abs(*config)
	if e != nil {
		return e
	}
	if (*install || *remove || *uninstall || *repair || *apply) && os.Geteuid() != 0 {
		return errors.New("esta operación requiere ejecutarse como root")
	}
	c, e := guard.LoadConfig(path)
	if *setup {
		if e == nil {
			return errors.New("la configuración ya existe; no se sobrescribe")
		}
		if !os.IsNotExist(e) {
			return e
		}
		_, e = guard.Setup(ctx, path, *dataDir, os.Stdin, os.Stdout, validateToken)
		return e
	}
	if *install && os.IsNotExist(e) {
		c, e = guard.Setup(ctx, path, *dataDir, os.Stdin, os.Stdout, validateToken)
	}
	if e != nil {
		return e
	}
	if *resetOwner {
		unlock, e := guard.LockRuntime(c.DataDir)
		if e != nil {
			return e
		}
		defer unlock()
		st, e := guard.OpenStore(c.DataDir)
		if e != nil {
			return e
		}
		return st.Change(func(s *guard.State) error {
			s.OwnerID = 0
			s.Sessions = map[string]guard.Session{}
			s.Failures = map[string]guard.Failure{}
			return nil
		})
	}
	if *install || *apply || *repair || *remove || *uninstall || *status {
		op := svcforge.OperationInstall
		switch {
		case *repair:
			op = svcforge.OperationRepair
		case *remove || *uninstall:
			op = svcforge.OperationRemove
		case *status:
			op = svcforge.OperationStatus
		}
		if *install || *apply {
			if e = validateToken(ctx, c.Token); e != nil {
				return e
			}
		}
		if *apply {
			timer := time.NewTimer(2 * time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		r, e := guard.Lifecycle(ctx, c, path, version, op)
		if e != nil {
			return e
		}
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	st, e := guard.OpenStore(c.DataDir)
	if e != nil {
		return e
	}
	unlock, e := guard.LockRuntime(c.DataDir)
	if e != nil {
		return e
	}
	defer unlock()
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetOutput(guard.NewRuntimeLog(c.DataDir))
	return guard.NewBot(guard.NewTelegram(c.Token), st, c, path, version).Run(ctx)
}
func validateToken(ctx context.Context, token string) error {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, e := guard.NewTelegram(token).Me(c)
	return e
}
