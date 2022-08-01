package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"
)

type LinkDump struct {
	Host      string
	Port      int
	EmailAddr string
	LinkMin   int
	LinkQueue []string
}

func (ld *LinkDump) router(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		switch r.Method {
		case "GET":
			if len(ld.LinkQueue) == 0 {
				fmt.Fprintf(w, "linkdump queue is empty!\n")
			} else {
				fmt.Fprintf(w, "linkdump queue:\n\n")
				for i, link := range ld.LinkQueue {
					fmt.Fprintf(w, "%d. %s\n", i+1, link)
				}
			}
		case "POST":
			ld.submit(w, r)
		default:
			http.Error(w, "403 Method Not Allowed", 403)
		}
	case "/dump":
		_, force := r.URL.Query()["force"]
		if ld.dump(force) {
			fmt.Fprintf(w, "Dump successful, email incoming!\n")
		} else {
			fmt.Fprintf(w, "Dump failed, see log for details\n")
		}
	default:
		http.Error(w, "403 Method Not Allowed", 403)
	}
}

func (ld *LinkDump) submit(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "400 Bad Request", 400)
		return
	}

	link := strings.TrimSpace(r.FormValue("link"))
	if link == "" {
		http.Error(w, "400 Bad Request (empty link is invalid)", 400)
		return
	}
	if link == "about:blank" {
		http.Error(w, "400 Bad Request (about:blank is invalid)", 400)
		return
	}

	log.Printf("Queueing link #%d: %s\n", len(ld.LinkQueue)+1, link)
	ld.LinkQueue = append(ld.LinkQueue, link)

	fmt.Fprintf(w, "Link #%d pushed to queue!\n", len(ld.LinkQueue))
}

func (ld *LinkDump) dump(force bool) bool {
	switch l := len(ld.LinkQueue); {
	case l == 0:
		log.Printf("Nothing to dump, link queue is empty\n")
		return false
	case l < ld.LinkMin && !force:
		log.Printf("Too few links (size %d, minimum %d) to dump\n", l, ld.LinkMin)
		return false
	default:
		log.Printf("Dumping link queue (size %d) to email\n", l)
		t := time.Now().Format(time.UnixDate)
		msg := fmt.Sprintf("Subject: linkdump - %s\nTo: %s\n\n", t, ld.EmailAddr)
		for i, link := range ld.LinkQueue {
			msg += fmt.Sprintf("%d. %s\n", i+1, link)
		}

		cmd := exec.Command("msmtp", ld.EmailAddr)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			log.Printf("Failed to create stdin pipe for mailer: %v\n", err)
			goto failure
		}

		go func() {
			defer stdin.Close()
			io.WriteString(stdin, msg)
		}()

		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to run mailer: %v\n", err)
			log.Printf("Failed mailer output: %q\n", out)
			goto failure
		} else {
			ld.LinkQueue = nil
			log.Printf("Successful dump, link queue reset (size %d)\n", len(ld.LinkQueue))
			return true
		}
	}
failure:
	log.Printf("Failed dump, link queue unchanged\n")
	return false
}

func main() {
	ld := LinkDump{}

	user, err := user.Current()
	var username string
	if err != nil {
		log.Printf("%v\n", err)
		username = "root"
	} else {
		username = user.Username
	}

	flag.StringVar(&ld.Host, "b", "localhost", "host")
	flag.IntVar(&ld.Port, "p", 1234, "port")
	flag.StringVar(&ld.EmailAddr, "e", username+"@localhost", "email address")
	flag.IntVar(&ld.LinkMin, "m", 10, "minimum links before dump")
	flag.Parse()

	log.Printf("Registering signal handlers\n")
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		for sig := range sigs {
			log.Printf("Caught %s\n", sig)
			ld.dump(sig != syscall.SIGUSR1)
			if sig == syscall.SIGTERM || sig == syscall.SIGINT {
				log.Fatal("Exiting...\n")
			}
		}
	}()

	http.HandleFunc("/", ld.router)
	addr := fmt.Sprintf("%s:%d", ld.Host, ld.Port)
	log.Printf("Binding to %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
