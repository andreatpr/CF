import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import NewCarousel from "../components/NewCarousel.jsx";
import "../styles/dashboard.css";

const TMDB_KEY = "450338d37aec7c0a08d23ffd741802c6";

export default function UserDashboard() {
  const navigate = useNavigate();
  const [recs, setRecs] = useState([]);
  const [obtain, setObtain] = useState(true);
  const [user, setUser] = useState(null);
  const [username, setUsername] = useState(null);

  // Estados nuevos
  const [selectedGenre, setSelectedGenre] = useState("All");
  const [limit, setLimit] = useState(10);
  const [genresList, setGenresList] = useState(["All"]);

  // 1. Cargar géneros dinámicos del backend al iniciar
  useEffect(() => {
    async function fetchGenres() {
      try {
        const res = await fetch("http://localhost:8080/genres");
        if (res.ok) {
          const data = await res.json();
          setGenresList(["All", ...data]);
        }
      } catch (error) {
        console.error("Error cargando géneros:", error);
      }
    }
    fetchGenres();
  }, []);

  // 2. Verificar usuario logueado
  useEffect(() => {
    const uid = localStorage.getItem("user_id");
    if (!uid) {
      navigate("/");
      return;
    }
    setUser(uid);
  }, [navigate]);

  // 3. Obtener nombre de usuario
  useEffect(() => {
    if (!user) return;
    async function fetchUsername() {
      const res = await fetch(`http://localhost:8900/user/${user}`);
      if (res.ok) {
        const data = await res.json();
        setUsername(data.nombre);
      }
    }
    fetchUsername();
  }, [user]);

  async function loadRecs() {
    setObtain(false);
    setRecs([]); // Limpiar anteriores visualmente

    // --- CORRECCIÓN IMPORTANTE AQUÍ ---
    // Agregamos genre y limit a la URL
    const res = await fetch(
      `http://localhost:8080/recommend?user_id=${user}&genre=${selectedGenre}&limit=${limit}`
    );
    const data = await res.json();

    // Enriquecer con TMDB (tu lógica original)
    const enriched = await Promise.all(
      data.map(async (m) => {
        const tmdb = await fetch(
          `https://api.themoviedb.org/3/search/movie?api_key=${TMDB_KEY}&query=${encodeURIComponent(m.title)}`
        );
        const json = await tmdb.json();

        let poster = null;
        let trailer = null;
        let overview = null;

        if (json.results && json.results.length > 0) {
          const best = json.results[0];
          if (best.poster_path) {
            poster = "https://image.tmdb.org/t/p/w500" + best.poster_path;
          }
          overview = best.overview;

          const vids = await fetch(
            `https://api.themoviedb.org/3/movie/${best.id}/videos?api_key=${TMDB_KEY}`
          );
          const vjson = await vids.json();
          if (vjson.results?.length > 0) {
            const yt = vjson.results.find((v) => v.site === "YouTube" && v.type === "Trailer");
            if (yt) {
              trailer = `https://www.youtube.com/watch?v=${yt.key}`;
            }
          }
        }

        return {
          ...m,
          poster: poster || "https://dummyimage.com/400x600/222/fff&text=NO+POSTER",
          trailer: trailer || "",
          overview: overview || "",
        };
      })
    );

    setRecs(enriched);
  }

  return (
    <div className="dashboard-container">
      {/* HEADER */}
      <div className="dashboard-header">
        <h1 className="dashboard-title">Bienvenido {username}!</h1>
        <button className="logout-btn" onClick={() => navigate("/")}>
          Cerrar Sesión
        </button>
      </div>

      <p className="dashboard-subtitle">
        Selecciona tus preferencias para ver recomendaciones personalizadas.
      </p>

      {/* --- CONTROLES DE FILTRO --- */}
      <div className="filters-container" style={{ display: "flex", gap: "10px", justifyContent: "center", marginBottom: "20px" }}>
        
        {/* Selector de GÉNERO (Dinámico) */}
        <select
          value={selectedGenre}
          onChange={(e) => {
            setSelectedGenre(e.target.value);
            setObtain(true); // Reactiva el botón para buscar de nuevo
          }}
          className="filter-select" // Puedes darle estilos en tu CSS
          style={{ padding: "8px", borderRadius: "5px" }}
        >
          {genresList.map((g) => (
            <option key={g} value={g}>
              {g}
            </option>
          ))}
        </select>

        {/* Selector de CANTIDAD (Límite) */}
        <select
          value={limit}
          onChange={(e) => {
            setLimit(Number(e.target.value));
            setObtain(true);
          }}
          className="filter-select"
          style={{ padding: "8px", borderRadius: "5px" }}
        >
          <option value="5">Top 5</option>
          <option value="10">Top 10</option>
          <option value="15">Top 15</option>
          <option value="20">Top 20</option>
        </select>
      </div>

      {/* BOTÓN DE ACCIÓN */}
      {obtain && (
        <button className="btn-recs" onClick={loadRecs}>
          Generar Top {limit} {selectedGenre !== "All" ? `de ${selectedGenre}` : ""}
        </button>
      )}

      {/* RESULTADOS */}
      {recs.length > 0 && <NewCarousel movies={recs} />}
    </div>
  );
}