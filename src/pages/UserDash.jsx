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

  useEffect(() => {
    const uid = localStorage.getItem("user_id");

    if (!uid) {
      navigate("/");
      return;
    }
    setUser(uid);
  }, [navigate]);

  useEffect(() => {
    if (!user) return;   // <- esperar user primero

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

    const res = await fetch(`http://localhost:8080/recommend?user_id=${user}`);
    const data = await res.json();

    // enrich con poster real
    const enriched = await Promise.all(
      data.map(async (m) => {
        const tmdb = await fetch(
        `https://api.themoviedb.org/3/search/movie?api_key=${TMDB_KEY}&query=${encodeURIComponent(m.title)}`
        );
        console.log("TMD",tmdb);

        const json = await tmdb.json();

        let poster = null;
        let trailer = null;
        let overview = null;
        const best = json.results[0]; // el más similar
    
        if(best.poster_path){
        poster = "https://image.tmdb.org/t/p/w500" + best.poster_path;
        }
        console.log("POSTER", poster);
        const vids = await fetch(
        `https://api.themoviedb.org/3/movie/${json.results[0].id}/videos?api_key=${TMDB_KEY}`
        );

        const vjson = await vids.json();

        if(vjson.results?.length > 0){
        const yt = vjson.results.find(v => v.site==="YouTube" && v.type==="Trailer");
        if(yt){
            trailer = `https://www.youtube.com/watch?v=${yt.key}`;
        }
        }
        
        overview = best.overview;
        return {
          ...m,
          poster: poster || "https://dummyimage.com/400x600/222/fff&text=NO+POSTER",
          trailer: trailer || "",
          overview: overview || ""
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
        En esta sección puedes ver recomendaciones personalizadas.
      </p>

      {obtain && (
        <button className="btn-recs" onClick={loadRecs}>
          Obtener recomendaciones
        </button>
      )}

      {recs.length > 0 && <NewCarousel movies={recs} />}
    </div>
  );
}
