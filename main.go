package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// Config holds all configuration for the link finder
type Config struct {
	SymlinksOnly  bool
	HardlinksOnly bool
	SearchPath    string
	SkipDirs      []string
	Timeout       time.Duration
}

// getInode safely extracts the inode number from a FileInfo object
func getInode(info fs.FileInfo) (uint64, error) {
	if runtime.GOOS == "windows" {
		return 0, fmt.Errorf("hard links detection not supported on Windows")
	}
	
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("unable to get system file info")
	}
	
	return stat.Ino, nil
}

// checkAndSendSymlink checks if a given path is a symbolic link pointing to the specified target.
// Uses canonical paths to properly compare targets.
func checkAndSendSymlink(ctx context.Context, path, target string, results chan<- string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		
		// Compare canonical paths
		targetCanonical, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		
		resolvedCanonical, err := filepath.Abs(resolved)
		if err != nil {
			return err
		}
		
		if resolvedCanonical == targetCanonical {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			
			select {
			case results <- fmt.Sprintf("%s (symlink) -> %s", path, linkTarget):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
}

// checkAndSendHardlink checks if the given file is a hardlink to the target file with targetInode.
func checkAndSendHardlink(ctx context.Context, path string, targetInode uint64, fileInfo fs.FileInfo, results chan<- string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		fileInode, err := getInode(fileInfo)
		if err != nil {
			return err
		}
		
		if fileInode == targetInode {
			select {
			case results <- fmt.Sprintf("%s (hardlink)", path):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
}

// shouldSkipDir checks if a directory should be skipped
func shouldSkipDir(path string, skipDirs []string) bool {
	for _, dir := range skipDirs {
		if path == dir {
			return true
		}
	}
	return false
}

// worker processes files from the jobs channel
func worker(ctx context.Context, id int, jobs <-chan string, results chan<- string, target string, targetInode uint64, config Config) {
	for {
		select {
		case <-ctx.Done():
			return
		case path, ok := <-jobs:
			if !ok {
				return
			}
			
			// Skip permission errors and process what we can
			fileInfo, err := os.Lstat(path)
			if err != nil {
				continue
			}

			if config.SymlinksOnly && fileInfo.Mode()&os.ModeSymlink != 0 {
				checkAndSendSymlink(ctx, path, target, results)
			} else if config.HardlinksOnly && !fileInfo.IsDir() && fileInfo.Mode().IsRegular() {
				checkAndSendHardlink(ctx, path, targetInode, fileInfo, results)
			} else if !config.SymlinksOnly && !config.HardlinksOnly {
				if fileInfo.Mode()&os.ModeSymlink != 0 {
					checkAndSendSymlink(ctx, path, target, results)
				} else if fileInfo.Mode().IsRegular() {
					checkAndSendHardlink(ctx, path, targetInode, fileInfo, results)
				}
			}
		}
	}
}

func main() {
	// Parse command line flags
	config := Config{
		SkipDirs: []string{"/proc", "/sys", "/dev"},
		Timeout:  30 * time.Minute,
	}
	
	flag.BoolVar(&config.SymlinksOnly, "s", false, "Find symlinks only")
	flag.BoolVar(&config.HardlinksOnly, "h", false, "Find hardlinks only")
	flag.StringVar(&config.SearchPath, "p", ".", "Path to start the search from")
	timeoutMin := flag.Int("t", 30, "Timeout in minutes")
	flag.Parse()
	
	config.Timeout = time.Duration(*timeoutMin) * time.Minute
	
	args := flag.Args()
	if len(args) != 1 {
		fmt.Println("Usage: lfinder [-s|-h] [-p path] [-t timeout] <target_file_name>")
		os.Exit(1)
	}
	
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	
	// Prepare target path
	target := args[0]
	targetPath := target
	if !filepath.IsAbs(target) {
		targetPath = filepath.Join(config.SearchPath, target)
	}
	
	// Get target file information
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		fmt.Printf("Error accessing target file: %v\n", err)
		os.Exit(1)
	}
	
	// Get target inode
	targetInode, err := getInode(targetInfo)
	if err != nil {
		fmt.Printf("Error getting target inode: %v\n", err)
		if runtime.GOOS == "windows" {
			fmt.Println("Note: Hard link detection is not supported on Windows")
		}
		if config.HardlinksOnly {
			os.Exit(1)
		}
	}
	
	// Create buffered channels
	jobs := make(chan string, 1000)
	results := make(chan string, 1000)
	
	// Start worker pool
	var wg sync.WaitGroup
	const numWorkers = 8
	
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(ctx, id, jobs, results, targetPath, targetInode, config)
		}(w)
	}
	
	// Walk the file system
	go func() {
		defer close(jobs)
		
		err := filepath.WalkDir(config.SearchPath, func(path string, d fs.DirEntry, err error) error {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			
			// Handle errors and skip problematic directories
			if err != nil {
				return nil
			}
			
			// Skip directories that might cause issues
			if d.IsDir() && shouldSkipDir(path, config.SkipDirs) {
				return fs.SkipDir
			}
			
			select {
			case jobs <- path:
			case <-ctx.Done():
				return ctx.Err()
			}
			
			return nil
		})
		
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			fmt.Printf("Error walking the path: %v\n", err)
		}
	}()
	
	// Wait for all workers to finish
	go func() {
		wg.Wait()
		close(results)
	}()
	
	// Print results
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Println("Search timed out")
			} else if ctx.Err() == context.Canceled {
				fmt.Println("Search canceled")
			}
			return
		case result, ok := <-results:
			if !ok {
				return
			}
			fmt.Println(result)
		}
	}
}
