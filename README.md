# Encore + Temporal Billing API

This project implements a **Billing/Fees API** using [Encore](https://encore.dev) and [Temporal](https://temporal.io). It demonstrates service orchestration using Temporal workflows, service-to-service communication within Encore, and handling stateful business processes such as adding line items to a bill, charging a bill, canceling an open bill, etc.


## Running the Project

To run this project locally, you'll need the following installed:

- [Go 1.24.2](https://go.dev/dl/)
- [Encore](https://encore.dev/)
- [Temporalite](https://github.com/temporalio/temporalite) (a lightweight local Temporal server)

### 1. Clone the repository

```bash
git clone https://github.com/nnobol/encore-temporal-go.git
```

### 2. Install dependencies

```bash
cd encore-temporal-go
go mod tidy
```

### 3. Start Temporalite

```bash
temporalite start --namespace default --ephemeral
```
Use --ephemeral flag to automatically wipe history between runs.

### 4. Start the Encore application (in a separate terminal)

```bash
encore run
```
This automatically starts all services and registers Temporal workflows and workers inside initService() — no main.go needed.

## API and Services Overview

### Billing Service Endpoints

| Action           | Method | Path                       |
|------------------|--------|----------------------------|
| Create bill      | POST   | `/bills`                   |
| Add line item    | POST   | `/bills/:bill_id/items`    |
| Charge bill      | POST   | `/bills/:bill_id/charge`   |
| Cancel bill      | POST   | `/bills/:bill_id/cancel`   |
| Get bill         | GET    | `/bills/:bill_id`          |

### Account Service Endpoints

| Action               | Method        | Path                          |
|----------------------|---------------|-------------------------------|
| Get balances         | GET           | `/balances`                   |
| Withdraw from account| POST          | `/balances/:curr/withdraw`    |
| Add balance          | RPC (private) | `account.AddBalance`          |

## Project Structure and Design Thoughts

### Why the `account` service?

The assignment focused on building a billing system, but I decided to introduce a lightweight `account` service to simulate service-to-service communication in Encore. This served multiple purposes:

- It made the `billing` workflow meaningful by **crediting the account** upon successful bill settlement.
- It allowed me to explore service-to-service communication within Encore, where `billing` asynchronously calls `account` to update balances.
- It added a natural feedback loop to billing: once we charge, we can see its effect via `GET /balances`.

> In real systems, `account` would likely persist data in a ledger database. Here, it uses in-memory maps for simplicity.

### Why in-memory currency & balances?

To keep the assignment focused on Temporal and Encore integration, I chose **not** to integrate a real DB or currency system. Instead:

- The list of supported currencies (USD, EUR, GEL) is hardcoded and parsed in a safe way.
- Balances in `account` are stored in a `map` protected by a mutex - thread-safe but ephemeral (data gets lost if services reload/restart).
- In real life, currencies and accounts would likely be tied together and stored in a database.
