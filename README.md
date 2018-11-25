Hi! 

Compile this by running `go build` in the directory you copied/cloned this repository to.
After that, you'll run it with `sudo ./gocache` - this binds to UDP port 53 (DNS) and begins
responding to DNS lookup requests. If the record does not exist in the programs memory, it will
attemt to recursively resolve the record and then store it into memory. 

This was a 2 hour mockup project because I had an idea watching a really dumb TV show. 
