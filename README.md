# LinkFinder

LinkFinder is a Go program designed to search through file systems for symbolic links (symlinks) and hard links related to a specified target file. This tool offers flexibility by allowing users to specify whether to search for symlinks, hard links, or both, and to define a starting path for the search.

## Features

- Search for symlinks and/or hard links throughout a file system.
- Configure the search to specifically find either symlinks, hard links, or both.
- Specify a starting path to narrow down the search area for efficiency.
- Concurrently process files using multiple goroutines for faster search operations.

## Usage

To use LinkFinder, compile the Go source file and run the executable with the desired command-line options.

```shell
lfinder [-s|-h] [-p path] <target_file_name>
```

### Command-Line Options

- `-s`: Find symlinks only. Searches for symbolic links that point to the specified target file.
- `-h`: Find hard links only. Searches for hard links that reference the same inode as the target file.
- `-p`: Specify the path to start the search from. Defaults to the root directory (`/`) if not set.

### Positional Arguments

- `<target_file_name>`: Specify the name of the target file to search for links. This argument is required.

### Example

Finding all symlinks pointing to `example.txt` starting from the `/home/user` directory:

```shell
lfinder -s -p /home/user example.txt
```

## Implementation Details

- Concurrently processes files by spawning multiple worker goroutines, enhancing the search speed.
- Utilizes channels for job distribution among workers and for collecting results.
- Handles both symlinks and hard links by checking file metadata and inode information.
- Employs a straightforward command-line interface using Go's `flag` package for ease of use.

## Building from Source

To compile LinkFinder from source, ensure you have a Go development environment set up, then run:

```shell
go build -o lfinder
```

## Dependencies

LinkFinder is built using the Go standard library only, with no external dependencies.

## Contributing

Contributions to LinkFinder are welcome! Whether it's feature enhancements, bug fixes, or documentation improvements, feel free to fork the repository and submit a pull request.

## License

LinkFinder is released under the MIT License. See the LICENSE file for more details.