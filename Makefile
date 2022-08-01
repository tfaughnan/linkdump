linkdump: linkdump.go
	env CGO_ENABLED=0 go build

clean:
	rm -f linkdump
