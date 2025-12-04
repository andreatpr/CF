import express from "express";
import cors from "cors";
import bcrypt from "bcrypt";
import { MongoClient } from "mongodb";

const app = express();
app.use(cors());
app.use(express.json());

// CORRECCIÓN 1: Usar la variable de entorno o el nombre del servicio Docker 'mongo'
const uri = process.env.MONGO_URI || "mongodb://mongo:27017";
const client = new MongoClient(uri);

// Conectamos a la BD. Si falla aquí, el server se detiene y sale error en logs
try {
  await client.connect();
  console.log("✅ Conectado a MongoDB en:", uri);
} catch (error) {
  console.error("❌ Error conectando a MongoDB:", error);
  process.exit(1);
}
const db = client.db("recsys");
const users = db.collection("users");

app.post("/register", async (req, res) => {
  const { email, password, nombre, role } = req.body;

  if (!email || !password || !nombre) {
    return res.status(400).json({ error: "nombre, email y password requeridos" });
  }

  const exists = await users.findOne({ email });
  if (exists) {
    return res.status(400).json({ error: "usuario ya existe" });
  }

  const count = await users.countDocuments();
  const nextId = count + 1;

  const hashed = await bcrypt.hash(password, 10);

  await users.insertOne({
    user_id: nextId,
    nombre,
    email,
    password: hashed,
    created_at: new Date(),
    role: role || "user" 
  });

  res.json({ user_id: nextId });
});

app.post("/login", async (req, res) => {
  const { email, password } = req.body;

  const doc = await users.findOne({ email });

  if (!doc) {
    return res.status(400).json({ error: "credenciales inválidas" });
  }

  const ok = await bcrypt.compare(password, doc.password);
  if (!ok) {
    return res.status(400).json({ error: "credenciales inválidas" });
  }

  console.log("LOGIN:", doc);

  res.json({
    user_id: doc.user_id,
    nombre: doc.nombre,
    role: doc.role   
  });
});

app.get("/user/:id", async (req, res) => {
  const userId = parseInt(req.params.id);

  const doc = await users.findOne(
    { user_id: userId },
    { projection: { _id: 0, nombre: 1, role: 1 } } 
  );

  if (!doc) {
    return res.status(404).json({ error: "usuario no encontrado" });
  }

  res.json(doc);
});


const PORT = 8900;
app.listen(PORT, () => console.log(`Auth service corriendo en puerto: ${PORT}`));