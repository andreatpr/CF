import { useState } from "react";
import { useNavigate } from "react-router-dom";
import "../styles/auth.css";

export default function Login() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const navigate = useNavigate();

  async function handleLogin() {
    try {
      const res = await fetch("http://localhost:8900/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });

      const data = await res.json();

      if (!res.ok || !data.user_id) {
        alert("Credenciales inválidas");
        return;
      }

      localStorage.setItem("user_id", data.user_id);
      localStorage.setItem("nombre", data.nombre);
      localStorage.setItem("role", data.role);

      if (data.role === "admin") {
        navigate("/admin");
      } else {
        navigate("/dashboard");
      }

    } catch (err) {
      console.error(err);
      alert("Error de conexión con el servidor");
    }
  }

  return (
    <div className="auth-container">
      <div className="auth-card">

        <h2 className="auth-title">Iniciar Sesión</h2>

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

        <button className="auth-button" onClick={handleLogin}>
          Entrar
        </button>

        <button className="auth-link" onClick={() => navigate("/register")}>
          Crear cuenta
        </button>

      </div>
    </div>
  );
}
