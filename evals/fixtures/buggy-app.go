package main

import "fmt"

func greet(users map[string]string) string {
	return users["user"]
}

func main() {
	users := map[string]string{"username": "hello"}
	fmt.Println(greet(users))
}
