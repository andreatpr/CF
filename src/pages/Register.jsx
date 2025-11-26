import { useState } from "react";
import { useNavigate } from "react-router-dom";
import "../styles/auth.css";

export default function Register() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [nombre, setNombre] = useState("");

  const navigate = useNavigate();

  async function handleRegister() {
    const res = await fetch("http://localhost:8900/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password, nombre })
    });

    const data = await res.json();

    if (!data.user_id) {
      alert("Error creando usuario");
      return;
    }

    // Auto-login
    localStorage.setItem("user_id", data.user_id);

    navigate("/dashboard");
  }

  return (
    <div className="auth-container">
      <div className="auth-card">

        <h2 className="auth-title">Crear Cuenta</h2>

        <input
          className="auth-input"
          placeholder="Nombre completo"
          value={nombre}
          onChange={(e) => setNombre(e.target.value)}
        />

        <input
          className="auth-input"
          type="email"
          placeholder="Correo electrónico"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />

        <input
          className="auth-input"
          type="password"
          placeholder="Contraseña"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />

        <button className="auth-button" onClick={handleRegister}>
          Registrarse
        </button>

        <button className="auth-link" onClick={() => navigate("/")}>
          Ya tengo cuenta
        </button>

      </div>
    </div>
  );
}
