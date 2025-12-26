# Go Realtime Chat

A small, opinionated skeleton for a realtime web chat application with a Go backend and a lightweight frontend. This repository contains an initial frontend layout (HTML/CSS/JS) and space to add a Go-based WebSocket server and persistence later.

**Crucial note — In progress:**  
This project is under active development and currently only provides a starting skeleton. It is not an MVP or production-ready reference. Expect missing features, changing APIs, and incomplete server code. If you clone the repo, treat it as work-in-progress learning material rather than a finished product.

I will try to add features slowly and update this README.md, so anyone can know which features are included and not included.

Currently, I just started the frontend. I will add the server logic, database, and others after some days.

**Notes after introduction of JWT:**
If you try to test the users and their conversations/messages, you can use two test accounts currently.
email: alice@example.test   password: password123
email: bob@example.test     password: password123

**Notes on running the server:**
I will add more details in future how to run the application properly (I apologize for inconvenience), but here are some important things to know.

If server says that it can't connect to the psql server, you can run "set -a; source .env; set +a" command.
As you can see, you need to setup .env file.
I left example .env file and you need to have your own psql login and password set there. Name it ".env", not ".env.example".
After setting it, the above command with "source .env" should run without issues.

Before running "go run main.go", be sure to run "go mod tidy" to have no issues with external packages.
After this, run it like: "export JWT_SECRET="insert_something_here(anything)" go run main.go".
