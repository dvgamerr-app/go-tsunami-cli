# go-tsunami-cli
Dataweave is cool but performance is suck.


## Benchmark
```shell
go test -bench=. -count=10

# Advance
go test -run=. -bench=. -benchtime=5s -count 10 -benchmem -cpuprofile=bin/cpu.out -memprofile=bin/mem.out -trace=bin/trace.out -o bin/tsunami.test.exe
go tool pprof -http :8080 bin/cpu.out
go tool pprof -http :8081 bin/mem.out
go tool trace bin/trace.out

rm -f bin/*.out
```