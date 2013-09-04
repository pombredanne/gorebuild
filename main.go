package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"code.google.com/p/go.exp/fsnotify"
)

var restart = flag.Bool("restart", false, "run process and restart it if the "+
	"target changes rather than invoking it when there are changes")

var target = flag.String("target", ".", "target to monitor for changes")

func main() {
	flag.Parse()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	args := flag.Args()

	tgt := *target
	if tgt == "." && *restart {
		// with -restart if -target isn't specified, use argv[0].
		tgt = args[0]
	}

	var watch_target string

	if _, err := os.Stat(tgt); err != nil {
		// File doesn't exist, look for it in $PATH
		path_tgt, err := exec.LookPath(tgt)
		if err != nil {
			log.Fatal("Can't find target %q", tgt)
		}
		tgt = path_tgt
	}

	if st, err := os.Stat(tgt); err != nil {
		log.Fatal(err)
	} else {
		watch_target = tgt
		if !st.IsDir() {
			watch_target = path.Dir(watch_target)
		}
	}

	log.Println("Monitoring", tgt)
	w.Watch(watch_target)

	if *restart {
		teardown, finished := start(args)
		for e := range w.Event {
			if e.Name != tgt {
				// Skip everything which isn't the file specified by -target
				continue
			}
			// Note: there is no event deduplication since it is assumed
			// that the target is a single file which will receive one event
			close(teardown)
			// Block until the other process terminated
			<-finished
			// TODO: debounce, run on up edge?
			teardown, finished = start(args)
		}
		return
	}

	if len(args) == 0 {
		args = []string{"go", "install"}
	}

	var teardown chan bool

	for e := range w.Event {
		if filepath.Dir(e.Name) == filepath.Base(e.Name) {
			// Special case to avoid changes to built go binary triggering its
			// own rebuild
			continue
		}
		if teardown != nil {
			// A closed teardown channel is a signal to not run the target
			// process or if it's running, to kill it.
			close(teardown)
			teardown = nil
		}
		teardown = invoke(args)
	}
}

func start(args []string) (chan struct{}, chan struct{}) {
	//args = append([]string{"-c", "eval \"$@\"", "--"}, args...)
	//p := exec.Command("bash", args...)
	p := exec.Command(args[0], args[1:]...)
	p.Stdout = os.Stdout
	p.Stderr = os.Stderr

	teardown, finished := make(chan struct{}), make(chan struct{})

	go func() {
		defer close(finished)

		select {
		case <-teardown:
			return
		case <-time.After(10 * time.Millisecond):
		}

		err := p.Start()
		if err != nil {
			panic(err)
		}

		go func() {
			<-teardown
			_ = p.Process.Signal(syscall.SIGTERM)
			log.Println("------- RESTARTING -------")
		}()

		done := make(chan struct{})

		go func() {
			err = p.Wait()
			close(done)
		}()
		<-done
	}()

	return teardown, finished
}

func invoke(args []string) chan bool {
	//args = append([]string{"-c", "eval \"$@\"", "--"}, args...)
	//p := exec.Command("bash", args...)
	p := exec.Command(args[0], args[1:]...)
	p.Stdout = os.Stdout
	p.Stderr = os.Stderr

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
			_ = p.Process.Kill()
		}
	}()

	return teardown
}
