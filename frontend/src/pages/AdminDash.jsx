import { useEffect, useState } from "react";
import "../styles/admin.css";

export default function AdminDashboard() {

  const [metrics, setMetrics] = useState(null);
  const [loading, setLoading] = useState(false);

  async function loadMetrics() {
    setLoading(true);
    try {
      const res = await fetch("http://localhost:8080/cluster");
      const data = await res.json();
      setMetrics(data);
    } catch (e) {
      console.error(e);
    }
    setLoading(false);
  }

  useEffect(() => {
    loadMetrics();
  }, []);

  return (
    <div className="admin-container">

      {/* HEADER */}
      <div className="admin-header">
        <h1 className="admin-title">Panel de Administración</h1>
      </div>

      <p style={{ opacity: 0.7 }}>
        Control del sistema distribuido • Estado en tiempo real
      </p>

      <button className="btn-primary" onClick={loadMetrics}>
        Refrescar métricas
      </button>

      {loading && <p style={{ marginTop: 20 }}>Cargando...</p>}

      {/* MÉTRICAS */}
      {metrics && (
        <>
          <h2 className="section-title">Uptime</h2>

          <div className="metric-card">
            <b>{metrics.uptime}</b>
          </div>

          <h2 className="section-title">Nodos activos</h2>

          <table className="node-table">
            <thead>
              <tr>
                <th>Nodo</th>
                <th>CPU</th>
                <th>Latencia</th>
              </tr>
            </thead>
            <tbody>
              {metrics.nodes.map(n => (
                <tr key={n.addr}>
                  <td>{n.addr}</td>
                  <td>{n.cpu}%</td>
                  <td>{n.latency_ms} ms</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}

    </div>
  );
}
