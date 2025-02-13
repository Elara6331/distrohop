/*
 * distrohop - A utility for correlating and identifying equivalent software
 * packages across different Linux distributions
 *
 * Copyright (C) 2025 Elara Ivy <elara@elara.ws>
 *
 * This file is part of distrohop.
 *
 * distrohop is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * distrohop is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with distrohop.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"encoding/json"
	"net/http"

	"go.elara.ws/salix"
)

type httpError struct {
	error
	StatusCode int
}

func handleErrJSON(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err != nil {
			if he, ok := err.(httpError); ok {
				w.WriteHeader(he.StatusCode)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"error": err.Error(),
			})
		}
	})
}

func handleErrGUI(ns *salix.Namespace, fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err != nil {
			if he, ok := err.(httpError); ok {
				w.WriteHeader(he.StatusCode)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
			ns.ExecuteTemplate(w, "error.html", map[string]any{
				"page": "Error",
				"err":  err.Error(),
			})
		}
	})
}
