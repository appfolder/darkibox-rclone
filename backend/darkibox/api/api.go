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
/*
name	"test"
code	"jp69dxx0mo"
fld_id	4587
*/

// Folder représente un dossier dans l'API darkibox.
type Folder struct {
	Name     string `json:"name"`      // Nom du dossier
	Code     string `json:"code"`      // Code (ID) du dossier
	FldID    string    `json:"fld_id"`    // Identifiant du dossier
}
type FolderResponse struct {
	Name     string `json:"name"`      // Nom du dossier
	Code     string `json:"code"`      // Code (ID) du dossier
	FldID    int    `json:"fld_id"`    // Identifiant du dossier
}

// File représente un fichier dans l'API darkibox.
type File struct {
	FileCode string `json:"file_code"`     // Code (ID) du fichier
	Title    string `json:"title"`         // Titre ou nom du fichier
	Size      int64  `json:"size"`  // Taille du fichier (en octets)
	Link     string `json:"link"`          // Lien direct vers le fichier
	Thumbnail string `json:"thumbnail"`    // Lien vers la miniature du fichier
	CanPlay  int    `json:"canplay"`       // Indique si le fichier peut être lu (1 = oui, 0 = non)
	Views    int    `json:"views"`        // Nombre de vues du fichier
	Uploaded string `json:"uploaded"`      // Date de téléchargement du fichier
	Public   int    `json:"public"`        // Indique si le fichier est public (1 = oui, 0 = non)
	FldID    string  `json:"fld_id"`       // Identifiant du dossier contenant le fichier
	Length   int    `json:"length"`       // Durée du fichier (pour les fichiers multimédias)
	Password string `json:"password"`     // Mot de passe du fichier (s'il est protégé)
}
type FileResponse struct {
	FileCode string `json:"file_code"`     // Code (ID) du fichier
	Title    string `json:"title"`         // Titre ou nom du fichier
	Name    string `json:"name"`
	Size      int64  `json:"size"`   // Taille du fichier (en octets)
	Link     string `json:"link"`          // Lien direct vers le fichier
	Thumbnail string `json:"thumbnail"`    // Lien vers la miniature du fichier
	CanPlay  int    `json:"canplay"`       // Indique si le fichier peut être lu (1 = oui, 0 = non)
	Views    int    `json:"views"`        // Nombre de vues du fichier
	Uploaded string `json:"uploaded"`      // Date de téléchargement du fichier
	Public   int    `json:"public"`        // Indique si le fichier est public (1 = oui, 0 = non)
	FldID    int    `json:"fld_id"`       // Identifiant du dossier contenant le fichier
	Length   int    `json:"length"`       // Durée du fichier (pour les fichiers multimédias)
	Password string `json:"password"`     // Mot de passe du fichier (s'il est protégé)
}
type FileListResponse struct {
	Msg        string `json:"msg"`         // Message de réponse (par ex. "OK")
	ServerTime string `json:"server_time"` // Heure du serveur
	Status     int    `json:"status"`      // Code de statut (200 = succès)
	Result     struct {
	    Folders []FolderResponse `json:"folders"`
		Files []FileResponse `json:"files"` // Liste des fichiers
		 // Liste des fichiers
		// Vous pouvez ajouter d'autres champs comme results_total, pages, etc. si nécessaire.
	} `json:"result"`
}
// UpdateFileInformation définit les informations nécessaires pour mettre à jour un fichier.
type UpdateFileInformationType struct {
	Token    string `json:"key"`     // Clé API darkibox
	FileCode string `json:"file_code"` // Code (ID) du fichier
	NewName  string `json:"file_title"`  // Nouveau nom du fichier
	NewOriginalName  string `json:"file_name"`  // Nouveau nom du fichier
	Public   string `json:"public"`    // "1" pour public, "0" pour privé
	Description string `json:"file_descr"` // Nouvelle description du fichier
	Adult   string `json:"file_adult"` // "1" pour adulte, "0" pour non-adulte
	PremiumOnly string `json:"file_premium_only"` // "1" pour premium uniquement, "0" pour tous
	CategoryID string `json:"cat_id"` // ID de la catégorie
	FldID string `json:"file_fld_id"` // ID du dossier
	Tags    string `json:"tags"`    // Tags associés au fichier
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