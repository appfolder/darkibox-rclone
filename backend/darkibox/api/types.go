// types.go
// Ce fichier définit les types principaux pour le backend darkibox utilisé par rclone.
package api

import (
	"regexp"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/encoder"
	"github.com/rclone/rclone/lib/rest"
)

// Options définit la configuration pour darkibox.
type Options struct {
	APIKey  string               `config:"api_key"`  // Votre clé API darkibox obtenue sur https://darkibox.com
	Private bool                 `config:"private"`  // Si true, les fichiers uploadés seront privés
	Enc     encoder.MultiEncoder `config:"encoding"` // Options d'encodage pour les noms de fichiers
}

// Fs représente le système de fichiers darkibox.
type Fs struct {
	root     string        // Chemin racine configuré
	name     string        // Nom du FS
	opt      Options       // Options de configuration
	features *fs.Features  // Fonctionnalités supportées
	srv      *rest.Client  // Client REST pour communiquer avec l'API darkibox
	pacer    *fs.Pacer     // Gestion du pacing pour les appels API
	idRegexp *regexp.Regexp// Expression régulière pour extraire l'ID d'un fichier depuis une URL
	public   string        // Valeur "1" pour public, "0" pour privé (selon l'option Private)
}

// Object représente un objet (fichier) stocké sur darkibox.
type Object struct {
	fs     *Fs    // Référence vers le système de fichiers parent
	remote string // Chemin distant complet (nom du fichier)
	size   int64  // Taille du fichier en octets
	code   string // Code (ID) du fichier sur darkibox
}
