package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// symlinksOnly represents a boolean flag that indicates whether only symbolic links should be considered.
// hardlinksOnly represents a boolean flag that indicates whether only hard links should be considered.
// searchPath represents the path to be searched for symlinks or hardlinks.
var (
	symlinksOnly  bool
	hardlinksOnly bool
	searchPath    string
)

// init is a function that initializes the command line flags for the program.
// It sets up the command line options for finding symlinks only, finding hardlinks only, and specifying the search path.
// Usage:
//
//	-s   Find symlinks only
//	-h   Find hardlinks only
//	-p   Path to start the search from
func init() {
	flag.BoolVar(&symlinksOnly, "s", false, "Find symlinks only")
	flag.BoolVar(&hardlinksOnly, "h", false, "Find hardlinks only")
	flag.StringVar(&searchPath, "p", "/", "Path to start the search from")
}

// checkAndSendSymlink checks if a given path is a symbolic link pointing to the specified target.
// If the path is a valid symbolic link and its resolved target matches the specified target,
// it sends the path along with its resolved target to the results channel.
func checkAndSendSymlink(path, target string, results chan<- string) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != target {
		return
	}
	linkTarget, _ := os.Readlink(path)
	results <- fmt.Sprintf("%s (symlink) -> %s", path, linkTarget)
}

// checkAndSendHardlink checks if the given file at `path` is a hardlink to the target file with `targetInode`.
// If it is a hardlink, it sends the path to the `results` channel.
func checkAndSendHardlink(path string, targetInode uint64, fileInfo os.FileInfo, results chan<- string) {
	if fileInfo.Sys().(*syscall.Stat_t).Ino == targetInode {
		results <- fmt.Sprintf("%s (hardlink)", path)
	}
}

func worker(id int, jobs <-chan string, results chan<- string, target string, targetInode uint64) {
	for path := range jobs {
		fileInfo, err := os.Lstat(path)
		if err != nil {
			continue // Skip on error
		}

		if symlinksOnly && fileInfo.Mode()&os.ModeSymlink != 0 {
			checkAndSendSymlink(path, target, results)
		} else if hardlinksOnly && !fileInfo.IsDir() && fileInfo.Mode().IsRegular() {
			checkAndSendHardlink(path, targetInode, fileInfo, results)
		} else if !symlinksOnly && !hardlinksOnly {
			if fileInfo.Mode()&os.ModeSymlink != 0 {
				checkAndSendSymlink(path, target, results)
			} else if fileInfo.Mode().IsRegular() {
				checkAndSendHardlink(path, targetInode, fileInfo, results)
			}
		}
	}
}

// main is the entry point of the program.
func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		fmt.Println("Usage: lfinder [-s|-h] [-p path] <target_file_name>")
		os.Exit(1)
	}
	target := args[0]

	targetInfo, err := os.Stat(filepath.Join(searchPath, target))
	if err != nil {
		fmt.Printf("Error accessing target file: %v\n", err)
		os.Exit(1)
	}
	targetInode := targetInfo.Sys().(*syscall.Stat_t).Ino

	jobs := make(chan string, 100)
	results := make(chan string, 100)

	var wg sync.WaitGroup
	const numWorkers = 8

	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(id, jobs, results, filepath.Join(searchPath, target), targetInode)
		}(w)
	}

	go func() {
		filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			jobs <- path
			return nil
		})
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	for result := range results {
		fmt.Println(result)
	}
}
