# datbox

A re-implementation of Disbox, a Discord-based file storage service. Aims to run locally with some extra features.

# File Versions
Datbox currently offers 2 file formats. The default is version 1.

## Continuous Deflate (Version 0)
This is the legacy file format of this program.
Due to poor early designs, some features are not supported with this format.
It's almost certainly always worse to choose this format.

### Pros
- **Fewer chunks uploaded**: This version uses a continuous deflate stream for uploading

### Cons
- **No encryption**: The format does not support storing password within itself
- **Expensive dynamic read**: To read a certain chunk, one must decode every chunk before it
- **Lack of header**: This format has no special headers, making it indistinguishable from a random file

## Chunked Deflate + Encrypt (Version 1)
The currently used file format.