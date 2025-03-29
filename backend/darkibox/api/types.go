// types.go
// Ce fichier définit les types principaux pour le backend darkibox utilisé par rclone.
package darkibox

import (
	"regexp"
	"time"
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

// Vérification que les interfaces de rclone sont bien satisfaites.
var (
	_ fs.Fs     = (*Fs)(nil)
	_ fs.Object = (*Object)(nil)
)


package api

import "encoding/json"

// Error représente une erreur renvoyée par l'API darkibox.
type Error struct {
	Code int    `json:"code"` // Code de l'erreur
	Msg  string `json:"msg"`  // Message décrivant l'erreur
}

// Error implémente l'interface error.
func (e Error) Error() string {
	return e.Msg
}

// MetadataRequestOptions définit les options pour la requête des métadonnées d'un dossier.
type MetadataRequestOptions struct {
	Limit  uint64 `json:"limit"`  // Nombre maximal d'éléments à retourner
	Offset uint64 `json:"offset"` // Décalage pour la pagination
}

// ReadMetadataResponse représente la réponse de l'API pour la lecture des métadonnées d'un dossier.
type ReadMetadataResponse struct {
	StatusCode int          `json:"status_code"` // Code de statut de l'API (0 = succès)
	Message    string       `json:"message"`     // Message associé au statut
	Data       MetadataData `json:"data"`        // Données des métadonnées
}

// MetadataData contient les informations sur le dossier courant, les sous-dossiers et les fichiers.
type MetadataData struct {
	CurrentFolder Folder   `json:"current_folder"` // Dossier courant
	Folders       []Folder `json:"folders"`        // Liste des sous-dossiers
	Files         []File   `json:"files"`          // Liste des fichiers
}

// Folder représente un dossier dans l'API darkibox.
type Folder struct {
	FolderID string `json:"folder_id"` // Identifiant du dossier
	Name     string `json:"name"`      // Nom du dossier
}

// File représente un fichier dans l'API darkibox.
type File struct {
	FileCode string `json:"file_code"`     // Code (ID) du fichier
	Title    string `json:"title"`         // Titre ou nom du fichier
	Size     int64  `json:"size,string"`   // Taille du fichier (en octets)
}

// UpdateFileInformation définit les informations nécessaires pour mettre à jour un fichier.
type UpdateFileInformation struct {
	Token    string `json:"token"`     // Clé API darkibox
	FileCode string `json:"file_code"` // Code (ID) du fichier
	NewName  string `json:"new_name"`  // Nouveau nom du fichier
	Public   string `json:"public"`    // "1" pour public, "0" pour privé
}

// UploadResponse représente la réponse de l'API après un upload de fichier.
type UploadResponse struct {
	Status int            `json:"status"` // Code de statut de l'API
	Msg    string         `json:"msg"`    // Message associé au statut
	Files  []UploadedFile `json:"files"`  // Informations sur le(s) fichier(s) uploadé(s)
}

// UploadedFile contient les informations d'un fichier uploadé.
type UploadedFile struct {
	URL      string `json:"url"`       // URL retournée après l'upload
	FileCode string `json:"file_code"` // Code (ID) du fichier
}

// UploadServerResponse représente la réponse de l'API pour obtenir l'URL du serveur d'upload.
type UploadServerResponse struct {
	Status int    `json:"status"` // Code de statut de l'API
	Msg    string `json:"msg"`    // Message associé au statut
	Result string `json:"result"` // URL du serveur d'upload
}

// SimpleResponse est une structure générique pour les réponses simples de l'API.
type SimpleResponse struct {
	Status int    `json:"status"` // Code de statut de l'API
	Msg    string `json:"msg"`    // Message associé au statut
}

// CloneResponse représente la réponse de l'API lors du clonage d'un fichier.
type CloneResponse struct {
	Status     int    `json:"status"`      // Code de statut de l'API
	Msg        string `json:"msg"`         // Message associé au statut
	ServerTime string `json:"server_time"` // Heure du serveur
	Result     struct {
		URL      string `json:"url"`       // URL du fichier cloné
		FileCode string `json:"filecode"`  // Code (ID) du fichier cloné
	} `json:"result"`
}

// UnmarshalError tente de désérialiser le corps d'une réponse en une erreur de l'API.
func UnmarshalError(data []byte) error {
	var apiErr Error
	if err := json.Unmarshal(data, &apiErr); err != nil {
		return err
	}
	return apiErr
}
