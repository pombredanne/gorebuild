package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"time"

	"code.google.com/p/go.exp/fsnotify"
)

var cmd = flag.String("cmd", "go install", "Command to run on changes")

// TODO(pwaller): File patterns to ignore?
//var ign = flag.String("ign", "-", "file to ignore")

func main() {
	flag.Parse()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	w.Watch(".")

	var teardown chan bool

	for _ = range w.Event {
		if teardown != nil {
			// A closed teardown channel is a signal to not run the target
			// process or if it's running, to kill it.
			close(teardown)
			teardown = nil
		}
		teardown = invoke()
	}
}

func invoke() chan bool {
	p := exec.Command("bash", "-c", *cmd)
	p.Stdout = os.Stdout
	p.Stdin = os.Stdin

	teardown := make(chan bool)

	go func() {
		// Wait 10ms in case another request comes in
		select {
		case <-teardown:
			return
		case <-time.After(10 * time.Millisecond):
		}

		go func() {
			log.Println("Starting ..")
			err := p.Start()
			if err != nil {
				panic(err)
			}
			_ = p.Wait() // Don't care about exit status
			log.Println(".. Finished")
			teardown <- true
		}()

		// Wait for process to finish or another start signal to come in
		finished := <-teardown

		if !finished {
			log.Println("! Another request came in, killing !")
			teardown = nil
			_ = p.Process.Signal(os.Kill) // Don't care if kill signal fails
		}
	}()

	return teardown
}
