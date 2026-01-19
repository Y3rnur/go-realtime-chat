# Go Realtime Chat

A small, opinionated skeleton for a realtime web chat application with a Go backend and a lightweight frontend. This repository contains an initial frontend layout (HTML/CSS/JS) and space to add a Go-based WebSocket server and persistence later.

**⚠️ Crucial note — In progress:**  
This project is under active development and currently only provides a starting skeleton. It is not an MVP or production-ready reference. Expect missing features, changing APIs, and incomplete server code. If you clone the repo, treat it as work-in-progress learning material rather than a finished product.

I will try to add features slowly and update this README.md, so anyone can know which features are included and not included.

**Current Features:**
Currently, this project has a backend server that serves some API calls for creating conversations and messages(and other handlers) and the use of tools like Websocket and Redis (for realtime communication features). Also, project features clean, minimalistic UI introduced on the frontend and the logic for sending certain requests/events to the backend and corresponding response change for the user's webpage. The project processes authentication via JWT and cookies. User registration is planned for future update.

**Notes after introduction of JWT:**
If you try to test the users and their conversations/messages, you can use six test(seed) accounts currently.
email: alice@example.test   password: password123
email: bob@example.test     password: password123
email: charlie@example.test password: password123
email: dave@example.test    password: password123
email: eve@example.test     password: password123
email: frank@example.test   password: password123

For now, there is no registration feature included. I will try to add it on future.

**Notes on running the server:**
I will add more details in future how to run the application properly (I apologize for inconvenience), but here are some important things to know.

LOCAL SETUP:
Currently, you can run the server locally. To run it, you must install below things:
- **Go** (1.20+) - for backend.
- **PostgreSQL** - for database and persistent data.
- **Redis**      - required for realtime event broadcasting and caching.

Before running the server, it is advisable to ensure that the correct environment files and variables are included. For that purpose, project has the ".env.example" file included on the root folder. Currently, there are two variables used, one for the DB/PostgreSQL connection(DATABASE_URL) and other for the JWT(JWT_SECRET). For the DATABASE_URL, you need to have your own Postgres user credentials and password. After determining those two, you must change the corresponding placeholders in brackets "<>". (When changing the values, don't include the brackets)
For JWT_SECRET, you can type anything you want (even leave that original text).
After changing them, make sure that you change the file name to ".env" (or copy them to the new file with the same name).

Now after we have ensured that above things are configured, there is the Makefile included in the project for a quick setup. You can use certain `make` commands to quickly build the DB schema, migration, and seeding the tables with sample data. You may do them separately, but there is the `make reset` command that automatically resets the whole DB and tables and also seeds the initial sample data. You can run `make help` for the short description of included `make` commands.

By running `make run` or `make start`, you can start the Go web server. You can look for terminal window's logs to be ensured that server is running.
When server starts successfully, you can visit the "http://localhost:8080" and use the above mentioned email and password for sample users and interact with the website.

CONTAINERIZED SETUP (in plan):
> **Note:** Docker support will come soon, which will automate the setup of the database and services with a single command.

In case you have troubles, I would recommend to find the solutions from the Internet.
