import express from "express";
import { history, record } from "./events";

const app = express();
app.use(express.json());

app.get("/health", (_req, res) => {
  res.json({ status: "ok" });
});

app.post("/api/parcels/:parcel/events", (req, res) => {
  try {
    res.status(201).json(record(req.params.parcel, req.body.status));
  } catch (e) {
    res.status(422).json({ error: (e as Error).message });
  }
});

app.get("/api/parcels/:parcel/events", (req, res) => {
  res.json(history(req.params.parcel));
});

app.listen(8000, () => console.log("tracking ready"));
