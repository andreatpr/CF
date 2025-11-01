package main

import (
	"math"
)

// NOTA: Para todas estas funciones, 'vecA' y 'vecB' son mapas que representan
// vectores dispersos. Por ejemplo, en un Item-based CF:
// vecA = map[UserID] -> Rating
// vecB = map[UserID] -> Rating

func cosineSimilarity(vecA, vecB map[int]float64) float64 {
	dotProduct := 0.0
	normA := 0.0
	normB := 0.0

	// Usamos el vector más corto para iterar y encontrar el producto punto
	// (Aunque iterar sobre A es suficiente si solo queremos el producto punto)
	for key, valA := range vecA {
		normA += valA * valA // Acumula para la magnitud de A

		// Si la clave (ej. UserID) también existe en B...
		if valB, ok := vecB[key]; ok {
			dotProduct += valA * valB // ...acumula el producto punto
		}
	}

	// Calcula la magnitud restante de B
	for _, valB := range vecB {
		normB += valB * valB
	}

	// Calcula las magnitudes
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)

	// Prevenir división por cero
	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (normA * normB)
}

func pearsonCorrelation(vecA, vecB map[int]float64) float64 {

	// --- Paso 1: Encontrar claves comunes (ej. co-raters) ---
	// También calculamos las medias *solo* de esos ítems comunes.

	commonKeys := []int{}
	sumA := 0.0
	sumB := 0.0

	for key, valA := range vecA {
		if valB, ok := vecB[key]; ok {
			commonKeys = append(commonKeys, key)
			sumA += valA
			sumB += valB
		}
	}

	n := float64(len(commonKeys))

	// Si no hay ítems en común, la correlación es 0.
	if n == 0 {
		return 0.0
	}

	// Medias de los ítems comunes
	meanA := sumA / n
	meanB := sumB / n

	// --- Paso 2: Calcular componentes de la fórmula de Pearson ---
	numerator := 0.0 // cov(A, B)
	sumSqA := 0.0    // σA^2
	sumSqB := 0.0    // σB^2

	for _, key := range commonKeys {
		// (Ai - meanA) * (Bi - meanB)
		centeredA := vecA[key] - meanA
		centeredB := vecB[key] - meanB

		numerator += centeredA * centeredB
		sumSqA += centeredA * centeredA
		sumSqB += centeredB * centeredB
	}

	// Prevenir división por cero
	denominator := math.Sqrt(sumSqA) * math.Sqrt(sumSqB)
	if denominator == 0 {
		return 0.0
	}

	return numerator / denominator
}

func jaccardIndex(vecA, vecB map[int]float64) float64 {

	// A y B son los conjuntos de usuarios que calificaron
	// (las claves de los mapas)

	intersectionSize := 0.0
	unionSet := make(map[int]bool)

	// Iteramos A
	for key := range vecA {
		unionSet[key] = true // Añadir al conjunto de unión

		if _, ok := vecB[key]; ok {
			intersectionSize++ // Clave está en A y B
		}
	}

	// Iteramos B
	for key := range vecB {
		unionSet[key] = true // Añadir al conjunto de unión (duplicados no importan)
	}

	unionSize := float64(len(unionSet))

	// Prevenir división por cero
	if unionSize == 0 {
		return 0.0
	}

	return intersectionSize / unionSize
}
