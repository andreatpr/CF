import "../styles/newCarousel.css";
import { useState } from "react";

export default function NewCarousel({ movies }) {

  const [hovered, setHovered] = useState(null);

  return (
    <div className="row-container">

      <h3 className="row-title">Recomendadas para ti</h3>

      <div className="row-scroller">

        {movies.map((m,i) => (
          
          <div 
            className="movie-card"
            key={i}
            onMouseEnter={()=>setHovered(i)}
            onMouseLeave={()=>setHovered(null)}
          >

            <img src={m.poster} className="poster"/>

            {hovered === i && (
              <div className="hover-info">

                <h4>{m.title}</h4>

                <p className="mini-score">{m.score.toFixed(3)}</p>

                {m.overview && <p className="overview">
                    {m.overview.slice(0,200)}
                </p>}

                {m.trailer &&
                  <a href={m.trailer} target="_blank" className="trailer-btn">
                    ▶ Ver trailer
                  </a>
                }

              </div>
            )}

          </div>

        ))}

      </div>

    </div>
  );
}
