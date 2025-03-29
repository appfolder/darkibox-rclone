// Package darkibox fournit une implémentation du backend darkibox pour rclone.
// Il adapte les appels de l’API darkibox (https://darkibox.com/api) aux interfaces de rclone.
package darkibox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/fs/fserrors"
	"github.com/rclone/rclone/fs/fshttp"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/lib/encoder"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/rclone/rclone/lib/random"
	"github.com/rclone/rclone/lib/rest"
)

const (
	apiBaseURL     = "https://darkibox.com/api"
	minSleep       = 400 * time.Millisecond
	maxSleep       = 5 * time.Second
	decayConstant  = 2
	attackConstant = 0
)
func init() {
	fs.Register(&fs.RegInfo{
		Name:        "darkibox",
		Description: "Darkibox",
		NewFs:       NewFs,
		Options: []fs.Option{
			{
				Help:      "Votre clé API darkibox.\n\nObtenez-la sur https://darkibox.com",
				Name:      "api_key",
				Sensitive: true,
			},
			{
				Help:     "Définir à true pour rendre les fichiers uploadés privés",
				Name:     "private",
				Advanced: true,
				Default:  false,
			},
			{
				Name:     config.ConfigEncoding,
				Help:     config.ConfigEncodingHelp,
				Advanced: true,
				// Longueur maximale du nom de fichier = 255 caractères
				Default: (encoder.Display |
					encoder.EncodeBackQuote |
					encoder.EncodeDoubleQuote |
					encoder.EncodeLtGt |
					encoder.EncodeLeftSpace |
					encoder.EncodeInvalidUtf8),
			},
		},
	})
}

// Options définit la configuration pour darkibox.
type Options struct {
	APIKey  string               `config:"api_key"`  // Votre clé API darkibox
	Private bool                 `config:"private"`  // Rendre les fichiers privés (true → privé)
	Enc     encoder.MultiEncoder `config:"encoding"` // Options d’encodage pour les noms
}

// Fs représente le système de fichiers darkibox.
type Fs struct {
	root     string
	name     string
	opt      Options
	features *fs.Features
	srv      *rest.Client
	pacer    *fs.Pacer
	// idRegexp extrait l'ID du fichier à partir d'une URL retournée lors de l'upload.
	idRegexp *regexp.Regexp
	// Valeur "public" à transmettre lors des mises à jour ("1" = public, "0" = privé)
	public string
}

// Object représente un objet (fichier) stocké sur darkibox.
type Object struct {
	fs     *Fs    // Référence au FS parent
	remote string // Chemin distant (nom du fichier complet)
	size   int64  // Taille du fichier en octets
	code   string // Code (ID) du fichier sur darkibox
}

func (f *Fs) Name() string {
	return f.name
}

// Root retourne le chemin racine configuré.
func (f *Fs) Root() string {
	return f.root
}

// String retourne une description du FS.
func (f *Fs) String() string {
	return fmt.Sprintf("Darkibox root '%s'", f.root)
}

// Precision indique que darkibox ne fournit pas de précision de date.
func (f *Fs) Precision() time.Duration {
	return fs.ModTimeNotSupported
}

// Hashes indique que darkibox ne fournit pas de hash.
func (f *Fs) Hashes() hash.Set {
	return hash.Set(hash.None)
}

// Features retourne les fonctionnalités supportées.
func (f *Fs) Features() *fs.Features {
	return f.features
}

// retryErrorCodes is a slice of error codes that we will retry
var retryErrorCodes = []int{
	429, // Too Many Requests.
	500, // Internal Server Error
	502, // Bad Gateway
	503, // Service Unavailable
	504, // Gateway Timeout
	509, // Bandwidth Limit Exceeded
}

// shouldRetry détermine si une requête doit être retentée.
func shouldRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if fserrors.ContextError(ctx, &err) {
		return false, err
	}
	retryCodes := []int{429, 500, 502, 503, 504, 509}
	if err != nil || (resp != nil && contains(retryCodes, resp.StatusCode)) {
		return true, err
	}
	return false, err
}

func (f *Fs) dirPath(file string) string {
	//return path.Join(f.diskRoot, file)
	if file == "" || file == "." {
		return "//" + f.root
	}
	return "//" + path.Join(f.root, file)
}
func (f *Fs) splitPathFull(pth string) (string, string) {
	fullPath := strings.Trim(path.Join(f.root, pth), "/")

	i := len(fullPath) - 1
	for i >= 0 && fullPath[i] != '/' {
		i--
	}

	if i < 0 {
		return "//" + fullPath[:i+1], fullPath[i+1:]
	}

	// do not include the / at the split
	return "//" + fullPath[:i], fullPath[i+1:]
}

func (f *Fs) splitPath(pth string) (string, string) {
	pth = strings.Trim(pth, "/")
	i := strings.LastIndex(pth, "/")
	if i < 0 {
		return "", pth
	}
	return pth[:i], pth[i+1:]
}

func NewFs(ctx context.Context, name, root string, m configmap.Mapper) (fs.Fs, error) {
	opt := new(Options)
	if err := configstruct.Set(m, opt); err != nil {
		return nil, err
	}

	f := &Fs{
    	name:     name,
    	root:     root,
    	opt:      *opt,
    	pacer:  fs.NewPacer(ctx, pacer.NewDefault(pacer.MinSleep(minSleep), pacer.MaxSleep(maxSleep), pacer.DecayConstant(decayConstant), pacer.AttackConstant(attackConstant))),
    	idRegexp: regexp.MustCompile(`([a-zA-Z0-9]+)$`),
    }
	// Pour darkibox, on considère "0" comme racine si le dossier est vide ou "."
	if root == "/" || root == "." {
		f.root = ""
	}
	// Définition des fonctionnalités supportées
	f.features = (&fs.Features{
		DuplicateFiles:          true,
		CanHaveEmptyDirectories: true,
		ReadMimeType:            false,
	}).Fill(ctx, f)
	if f.opt.Private {
		f.public = "0"
	} else {
		f.public = "1"
	}
	client := fshttp.NewClient(ctx)
	f.srv = rest.NewClient(client).SetRoot(apiBaseURL)
	return f, nil
}

func (f *Fs) decodeError(resp *http.Response, response any) (err error) {
	defer fs.CheckClose(resp.Body, &err)

	// Lecture complète du corps de la réponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	// Essayer de désérialiser dans la structure attendue
	err = json.Unmarshal(body, response)
	if err == nil {
		return nil
	}
	// En cas d'échec, tenter de désérialiser dans la structure d'erreur
	var apiErr api.Error
	err = json.Unmarshal(body, &apiErr)
	if err != nil {
		return err
	}
	return apiErr
}

func (f *Fs) List(ctx context.Context, dir string) (entries fs.DirEntries, err error) {
	// Pour darkibox, "fld_id" représente le dossier ; on utilise "0" si aucun dossier n’est spécifié.
	folder := "0"
	if dir != "" {
		folder = dir
	}
	params := url.Values{}
	params.Set("key", f.opt.APIKey)
	params.Set("fld_id", folder)
	params.Set("files", "1")
	opts := rest.Opts{
		Method:     "GET",
		Path:       "/file/list",
		Parameters: params,
	}
	var resp *http.Response
	var listResp FileListResponse
	err = f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &listResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return nil, err
	}
	if listResp.Status != 200 {
		return nil, errors.New(listResp.Msg)
	}
	// Parcourir la liste des fichiers et créer des objets
	for _, file := range listResp.Result.Files {
		remote := path.Join(dir, f.opt.Enc.ToStandardName(file.Title))
		// La taille n’est pas toujours renseignée dans file/list → ici on met 0
		o := &Object{
			fs:     f,
			remote: remote,
			size:   0,
			code:   file.FileCode,
		}
		entries = append(entries, o)
	}
	return entries, nil
}

// NewObject recherche et retourne l'objet correspondant au chemin distant.
func (f *Fs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	parent, _ := f.splitPath(remote)
	entries, err := f.List(ctx, parent)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if o, ok := entry.(*Object); ok {
			if o.Remote() == remote {
				return o, nil
			}
		}
	}
	return nil, fs.ErrorObjectNotFound
}
func (f *Fs) uploadFile(ctx context.Context, in io.Reader, size int64, filename, uploadURL string, options ...fs.OpenOption) (*UploadResponse, error) {
	opts := rest.Opts{
		Method:                 "POST",
		RootURL:                uploadURL,
		Body:                   in,
		ContentLength:          &size,
		MultipartContentName:   "file",
		MultipartFileName:      filename,
		Options:                options,
	}
	var resp *http.Response
	var upResp UploadResponse
	err := f.pacer.CallNoRetry(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &upResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return nil, fmt.Errorf("échec de l'upload du fichier: %w", err)
	}
	return &upResp, nil
}
func (f *Fs) move(ctx context.Context, toFolder, fileID string) error {
	params := url.Values{}
	params.Set("key", f.opt.APIKey)
	params.Set("file_code", fileID)
	params.Set("to_folder", toFolder)
	opts := rest.Opts{
		Method:     "GET",
		Path:       "/file/move",
		Parameters: params,
	}
	var resp *http.Response
	var mvResp SimpleResponse
	err := f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &mvResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return err
	}
	if mvResp.Status != 200 {
		return errors.New(mvResp.Msg)
	}
	return nil
}
func (f *Fs) putUnchecked(ctx context.Context, in io.Reader, remote string, size int64, options ...fs.OpenOption) error {
	if size == 0 {
		return fs.ErrorCantUploadEmptyFiles
	}
	// Obtenir l’URL du serveur d’upload via l’API darkibox.
	uploadURL, err := f.getUploadServer(ctx)
	if err != nil {
		return err
	}
	// Générer un nom temporaire
	tmpName := "rcloneTemp" + randomString(8)
	uploadResp, err := f.uploadFile(ctx, in, size, tmpName, uploadURL, options...)
	if err != nil {
		return err
	}
	if len(uploadResp.Files) != 1 {
		return errors.New("réponse d'upload inattendue")
	}
	// Extraire l’ID du fichier à partir de l’URL retournée
	match := f.idRegexp.FindStringSubmatch(uploadResp.Files[0].URL)
	if len(match) < 2 {
		return errors.New("impossible d'extraire l'ID du fichier uploadé")
	}
	fileID := match[1]
	// Gérer le déplacement dans le dossier cible (si nécessaire)
	base, leaf := f.splitPath(remote)
	fullBase := f.dirPath(base)
	if fullBase != "0" {
		// Créer le dossier s'il n'existe pas
		if err = f.Mkdir(ctx, base); err != nil {
			return err
		}
		// Déplacer le fichier vers le dossier cible
		if err = f.move(ctx, fullBase, fileID); err != nil {
			return err
		}
	}
	// Renommer le fichier avec le nom final
	if err = f.updateFileInformation(ctx, fileID, f.opt.Enc.FromStandardName(leaf)); err != nil {
		return err
	}
	return nil
}


func (f *Fs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	existingObj, err := f.NewObject(ctx, src.Remote())
	switch err {
	case nil:
		return existingObj, existingObj.Update(ctx, in, src, options...)
	case fs.ErrorObjectNotFound:
		return f.PutUnchecked(ctx, in, src, options...)
	default:
		return nil, err
	}
}

// PutUnchecked effectue un upload sans vérifier l’existence préalable.
func (f *Fs) PutUnchecked(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	err := f.putUnchecked(ctx, in, src.Remote(), src.Size(), options...)
	if err != nil {
		return nil, err
	}
	return f.NewObject(ctx, src.Remote())
}

func (f *Fs) CreateDir(ctx context.Context, base string, leaf string) (err error) {
	// Si base est vide, on considère que le dossier parent est la racine ("0")
	if strings.TrimSpace(base) == "" {
		base = "0"
	}

	// Préparer les paramètres de la requête
	params := url.Values{}
	params.Set("key", f.opt.APIKey)
	params.Set("name", f.opt.Enc.FromStandardName(leaf))
	// On encode le chemin du dossier parent
	params.Set("parent_id", f.opt.Enc.FromStandardPath(base))

	opts := rest.Opts{
		Method:     "GET",            // Pour darkibox, la création se fait via GET
		Path:       "/folder/create", // Endpoint de création de dossier
		Parameters: params,
	}

	var resp *http.Response
	var folderResp struct {
		Msg        string `json:"msg"`
		ServerTime string `json:"server_time"`
		Status     int    `json:"status"`
		Result     struct {
			FldID string `json:"fld_id"`
		} `json:"result"`
	}

	// Appel de l'API avec gestion du pacing et des retards éventuels
	err = f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &folderResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return err
	}

	// Si le dossier existe déjà, on ignore l'erreur
	if folderResp.Status != 200 && !strings.Contains(folderResp.Msg, "already exists") {
		return errors.New(folderResp.Msg)
	}
	return nil
}


func (f *Fs) mkDirs(ctx context.Context, path string) (err error) {
	// Supprimer les slashs en début et fin de chaîne
	dirs := strings.Split(path, "/")
	var base string
	for _, element := range dirs {
		// Pour chaque dossier non vide, on le crée dans le dossier parent courant
		if element != "" {
			err = f.CreateDir(ctx, base, element)
			if err != nil {
				return err
			}
			base += "/" + element
		}
	}
	return nil
}
func (f *Fs) Mkdir(ctx context.Context, dir string) error {
	if dir == "" || dir == "0" {
		return nil
	}
	params := url.Values{}
	params.Set("key", f.opt.APIKey)
	// On encode le nom du dossier
	params.Set("name", f.opt.Enc.FromStandardName(dir))
	// On utilise "0" comme parent (racine)
	params.Set("parent_id", "0")
	opts := rest.Opts{
		Method:     "GET",
		Path:       "/folder/create",
		Parameters: params,
	}
	var resp *http.Response
	var folderResp struct {
		Msg        string `json:"msg"`
		ServerTime string `json:"server_time"`
		Status     int    `json:"status"`
		Result     struct {
			FldID string `json:"fld_id"`
		} `json:"result"`
	}
	err := f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &folderResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return err
	}
	// On ignore l’erreur si le dossier existe déjà
	if folderResp.Status != 200 && !strings.Contains(folderResp.Result.FldID, "already exists") {
		return errors.New(folderResp.Msg)
	}
	return nil
}
func (f *Fs) Mkdir(ctx context.Context, dir string) error {
	if dir == "" || dir == "." {
		return f.mkDirs(ctx, "0")
	}
	return f.mkDirs(ctx, path.Join("0", dir))
}
func (f *Fs) purge(ctx context.Context, fldID string) error {
	var resp *http.Response
	opts := rest.Opts{
		Method: "GET",
		Path:   "/folder/delete",
		Parameters: url.Values{
			"key":    []string{f.opt.APIKey},
			"fld_id": []string{fldID},
		},
	}
	var delResp struct {
		Msg        string `json:"msg"`
		ServerTime string `json:"server_time"`
		Status     int    `json:"status"`
		Result     string `json:"result"`
	}
	err := f.pacer.Call(func() (bool, error) {
		resp, err := f.srv.CallJSON(ctx, &opts, nil, &delResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return err
	}
	if delResp.Status != 200 {
		return fmt.Errorf("suppression du dossier : erreur API %d - %s", delResp.Status, delResp.Msg)
	}
	return nil
}

func (f *Fs) readMetaDataForPath(ctx context.Context, fldID string, options *api.MetadataRequestOptions) (*api.ReadMetadataResponse, error) {
	// Préparation des paramètres pour l'appel à l'API darkibox
	opts := rest.Opts{
		Method: "GET",
		Path:   "/folder/list",
		Parameters: url.Values{
			"key":    []string{f.opt.APIKey}, // Utilisation de la clé API darkibox
			"fld_id": []string{fldID},
			"files":  []string{"1"},
			"limit":  []string{strconv.FormatUint(options.Limit, 10)},
		},
	}

	// Ajout de l'offset si différent de zéro
	if options.Offset != 0 {
		opts.Parameters.Set("offset", strconv.FormatUint(options.Offset, 10))
	}

	var err error
	var info api.ReadMetadataResponse
	var resp *http.Response

	// Appel de l'API via le pacer pour gérer les délais d'attente et les retards éventuels
	err = f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.Call(ctx, &opts)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return nil, err
	}

	// Décodage de la réponse pour détecter une éventuelle erreur
	err = f.decodeError(resp, &info)
	if err != nil {
		return nil, err
	}

	// Vérification du code de statut retourné par l'API
	if info.StatusCode != 0 {
		return nil, errors.New(info.Message)
	}

	return &info, nil
}

func (f *Fs) Rmdir(ctx context.Context, dir string) error {
	// Récupération des métadonnées du dossier
	info, err := f.readMetaDataForPath(ctx, dir, &api.MetadataRequestOptions{Limit: 10})
	if err != nil {
		return err
	}
	// Vérification que le dossier est bien vide (aucun sous-dossier ou fichier présent)
	if len(info.Data.Folders) > 0 || len(info.Data.Files) > 0 {
		return fs.ErrorDirectoryNotEmpty
	}
	// Suppression du dossier en appelant la fonction purge avec l'ID du dossier courant
	return f.purge(ctx, info.Data.CurrentFolder.FolderID)
}

func (f *Fs) updateFileInformation(ctx context.Context, fileID, newName string) error {
	params := url.Values{}
	params.Set("key", f.opt.APIKey)
	params.Set("file_code", fileID)
	params.Set("file_title", newName)
	opts := rest.Opts{
		Method:     "GET",
		Path:       "/file/edit",
		Parameters: params,
	}
	var resp *http.Response
	var editResp SimpleResponse
	err := f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &editResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return err
	}
	if editResp.Status != 200 {
		return errors.New(editResp.Msg)
	}
	return nil
}

func (f *Fs) getUploadServer(ctx context.Context) (string, error) {
	params := url.Values{}
	params.Set("key", f.opt.APIKey)
	opts := rest.Opts{
		Method:     "GET",
		Path:       "/upload/server",
		Parameters: params,
	}
	var resp *http.Response
	var usResp UploadServerResponse
	err := f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &usResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return "", err
	}
	if usResp.Status != 200 {
		return "", errors.New(usResp.Msg)
	}
	return usResp.Result, nil
}
func (f *Fs) renameDir(ctx context.Context, folderID uint64, newName string) (err error) {
	// Préparer les paramètres de la requête
	params := url.Values{}
	params.Set("key", f.opt.APIKey)
	params.Set("fld_id", folderID)
	params.Set("name", newName)

	opts := rest.Opts{
		Method:     "GET",            // L'API "folder/edit" s'exécute via GET
		Path:       "/folder/edit",   // Endpoint pour éditer (renommer) un dossier
		Parameters: params,
	}

	var resp *http.Response
	var editResp struct {
		Msg        string `json:"msg"`
		ServerTime string `json:"server_time"`
		Status     int    `json:"status"`
		Result     string `json:"result"`
	}

	// Appel de l'API avec gestion du pacing et des éventuels retards
	err = f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &editResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return err
	}
	// Vérifier que l'API retourne un status 200 (succès)
	if editResp.Status != 200 {
		return fmt.Errorf("renameDir: erreur API %d - %s", editResp.Status, editResp.Msg)
	}
	return nil
}
func (f *Fs) DirMove(ctx context.Context, src fs.Fs, srcRemote, dstRemote string) error {
	// Vérifier que le FS source est du même type
	srcFs, ok := src.(*Fs)
	if !ok {
		fs.Debugf(src, "Impossible de déplacer le dossier – types de stockage différents")
		return fs.ErrorCantDirMove
	}

	// Récupérer les métadonnées du dossier source via Folder List
	srcPath := srcFs.dirPath(srcRemote)
	srcInfo, err := f.readMetaDataForPath(ctx, srcPath, &api.MetadataRequestOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("dirmove : dossier source introuvable : %w", err)
	}

	// Vérifier que la destination n'existe pas déjà
	dstPath := f.dirPath(dstRemote)
	_, err = f.readMetaDataForPath(ctx, dstPath, &api.MetadataRequestOptions{Limit: 1})
	if err == nil {
		return fs.ErrorDirExists
	}

	// Créer le répertoire parent de la destination
	dstBase, dstName := f.splitPathFull(dstRemote)
	if err = f.mkDirs(ctx, strings.Trim(dstBase, "/")); err != nil {
		return fmt.Errorf("dirmove : échec de la création des dossiers de destination : %w", err)
	}

	// Récupérer les métadonnées du dossier parent de destination
	dstInfo, err := f.readMetaDataForPath(ctx, dstBase, &api.MetadataRequestOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("dirmove : échec de lecture du dossier de destination : %w", err)
	}

	// Décomposer le chemin source pour obtenir le nom (leaf)
	srcBase, srcName := srcFs.splitPathFull(srcRemote)
	needRename := srcName != dstName
	needMove := srcBase != dstBase

	// Si un renommage est nécessaire, renommer temporairement le dossier source
	if needRename {
		tmpName := "rcloneTemp" + randomString(8)
		err = f.renameDir(ctx, srcInfo.Data.CurrentFolder.FolderID, tmpName)
		if err != nil {
			return fmt.Errorf("dirmove : échec du renommage temporaire : %w", err)
		}
	}

	// Effectuer le déplacement en mettant à jour le parent du dossier source
	if needMove {
		// Utiliser l'API "folder/edit" pour changer le dossier parent
		params := url.Values{}
		params.Set("key", f.opt.APIKey)
		params.Set("fld_id", srcInfo.Data.CurrentFolder.FolderID)
		// Nouveau parent obtenu depuis dstInfo
		params.Set("parent_id", dstInfo.Data.CurrentFolder.FolderID)
		// Conserver le nom actuel (temporaire ou non)
		params.Set("name", srcInfo.Data.CurrentFolder.Name)

		opts := rest.Opts{
			Method:     "GET", // L'API "folder/edit" s'exécute via GET
			Path:       "/folder/edit",
			Parameters: params,
		}

		var resp *http.Response
		var editResp struct {
			Msg        string `json:"msg"`
			ServerTime string `json:"server_time"`
			Status     int    `json:"status"`
			Result     json.RawMessage `json:"result"`
		}

		err = f.pacer.Call(func() (bool, error) {
			resp, err = f.srv.CallJSON(ctx, &opts, nil, &editResp)
			return shouldRetry(ctx, resp, err)
		})
		if err != nil {
			return fmt.Errorf("dirmove : échec du déplacement du dossier : %w", err)
		}
		if editResp.Status != 200 {
			return fmt.Errorf("dirmove : erreur API lors du déplacement : %d - %s", editResp.Status, editResp.Msg)
		}
	}

	// Si nécessaire, renommer le dossier vers le nom final
	if needRename {
		err = f.renameDir(ctx, srcInfo.Data.CurrentFolder.FolderID, dstName)
		if err != nil {
			return fmt.Errorf("dirmove : échec du renommage final : %w", err)
		}
	}

	return nil
}
func (f *Fs) copy(ctx context.Context, dstPath string, fileID string, newTitle string) error {
	// Récupération des métadonnées du dossier de destination via Folder List
	meta, err := f.readMetaDataForPath(ctx, dstPath, &api.MetadataRequestOptions{Limit: 10})
	if err != nil {
		return err
	}

	// Préparation des paramètres pour l'appel à l'endpoint "file/clone"
	params := url.Values{}
	params.Set("key", f.opt.APIKey)
	params.Set("file_code", fileID)
	params.Set("file_title", newTitle)
	// Utilisation de l'ID du dossier de destination obtenu via Folder List
	params.Set("fld_id", meta.Data.CurrentFolder.FolderID)

	opts := rest.Opts{
		Method:     "GET",         // L'API clone se fait via GET
		Path:       "/file/clone", // Endpoint pour cloner un fichier
		Parameters: params,
	}

	var resp *http.Response
	var cloneResp struct {
		Msg        string `json:"msg"`
		ServerTime string `json:"server_time"`
		Status     int    `json:"status"`
		Result     struct {
			URL      string `json:"url"`
			FileCode string `json:"filecode"`
		} `json:"result"`
	}

	err = f.pacer.Call(func() (bool, error) {
		resp, err = f.srv.CallJSON(ctx, &opts, nil, &cloneResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return fmt.Errorf("impossible de cloner le fichier : %w", err)
	}
	if cloneResp.Status != 200 {
		return fmt.Errorf("clone : erreur API %d - %s", cloneResp.Status, cloneResp.Msg)
	}
	return nil
}

// Copy copie l'objet src vers la destination indiquée en effectuant une copie côté serveur via l'API darkibox.
func (f *Fs) Copy(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	// Vérifier que l'objet source est bien du même type
	srcObj, ok := src.(*Object)
	if !ok {
		fs.Debugf(src, "Impossible de copier – types de stockage différents")
		return nil, fs.ErrorCantMove
	}

	// Extraire le nom du fichier source et décomposer le chemin de destination
	_, srcLeaf := f.splitPath(src.Remote())
	dstBase, dstLeaf := f.splitPath(remote)
	needRename := srcLeaf != dstLeaf

	// Créer les dossiers de destination si nécessaire
	err := f.mkDirs(ctx, path.Join(f.root, dstBase))
	if err != nil {
		return nil, fmt.Errorf("copie : échec de la création des dossiers de destination : %w", err)
	}

	// Effectuer la copie côté serveur en utilisant le nom source pour le clone initial
	err = f.copy(ctx, f.dirPath(dstBase), srcObj.code, srcLeaf)
	if err != nil {
		return nil, err
	}

	// Retrouver le nouvel objet copié dans le dossier de destination
	newObj, err := f.NewObject(ctx, path.Join(dstBase, srcLeaf))
	if err != nil {
		return nil, fmt.Errorf("copie : impossible de retrouver l'objet copié : %w", err)
	}

	// Si le nom de destination final diffère, renommer le fichier
	if needRename {
		err := f.updateFileInformation(ctx, &api.UpdateFileInformation{
			Token:    f.opt.APIKey,
			FileCode: newObj.(*Object).code,
			NewName:  f.opt.Enc.FromStandardName(dstLeaf),
			Public:   f.public,
		})
		if err != nil {
			return nil, fmt.Errorf("copie : échec du renommage final : %w", err)
		}
		newObj.(*Object).remote = remote
	}

	return newObj, nil
}

// Fs returns the parent Fs
func (o *Object) Fs() fs.Info {
	return o.fs
}

// String retourne une représentation textuelle de l’objet.
func (o *Object) String() string {
	return o.remote
}

// Remote retourne le chemin distant.
func (o *Object) Remote() string {
	return o.remote
}

func (o *Object) ModTime(ctx context.Context) time.Time {
	ci := fs.GetConfig(ctx)
	return time.Time(ci.DefaultTime)
}

// Size returns the size of an object in bytes
func (o *Object) Size() int64 {
	return o.size
}

// Hash returns the Md5sum of an object returning a lowercase hex string
func (o *Object) Hash(ctx context.Context, t hash.Type) (string, error) {
	return "", hash.ErrUnsupported
}

// ID returns the ID of the Object if known, or "" if not
func (o *Object) ID() string {
	return o.code
}

// Storable returns whether this object is storable
func (o *Object) Storable() bool {
	return true
}

// SetModTime n’est pas supporté.
func (o *Object) SetModTime(ctx context.Context, modTime time.Time) error {
	return fs.ErrorCantSetModTime
}

// Open ouvre l’objet pour lecture en récupérant le lien direct via l’API "file/direct_link".
func (o *Object) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	params := url.Values{}
	params.Set("key", o.fs.opt.APIKey)
	params.Set("file_code", o.code)
	// Ici, on demande la version originale ("q" = "o")
	params.Set("q", "o")
	opts := rest.Opts{
		Method:     "GET",
		Path:       "/file/direct_link",
		Parameters: params,
	}
	var resp *http.Response
	var directResp struct {
		Msg        string `json:"msg"`
		ServerTime string `json:"server_time"`
		Status     int    `json:"status"`
		Result     struct {
			Versions []struct {
				URL  string `json:"url"`
				Name string `json:"name"`
				Size string `json:"size"`
			} `json:"versions"`
		} `json:"result"`
	}
	err := o.fs.pacer.Call(func() (bool, error) {
		resp, err = o.fs.srv.CallJSON(ctx, &opts, nil, &directResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return nil, err
	}
	if directResp.Status != 200 {
		return nil, errors.New(directResp.Msg)
	}
	if len(directResp.Result.Versions) == 0 {
		return nil, errors.New("aucune version disponible")
	}
	downloadURL := directResp.Result.Versions[0].URL
	opts = rest.Opts{
		Method:  "GET",
		RootURL: downloadURL,
	}
	err = o.fs.pacer.Call(func() (bool, error) {
		resp, err = o.fs.srv.Call(ctx, &opts)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (o *Object) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	// Vérifier que la taille du fichier est connue
	if src.Size() < 0 {
		return errors.New("refus de mettre à jour avec une taille inconnue")
	}
	// Upload du nouveau fichier sans vérification préalable
	if err := o.fs.putUnchecked(ctx, in, o.Remote(), src.Size(), options...); err != nil {
		return err
	}
	// Suppression de l'ancienne version de l'objet
	if err := o.Remove(ctx); err != nil {
		return fmt.Errorf("échec de la suppression de l'ancienne version: %w", err)
	}
	// Récupération du nouvel objet pour mettre à jour la structure en mémoire
	newObj, err := o.fs.NewObject(ctx, o.Remote())
	if err != nil {
		return err
	}
	newO, ok := newObj.(*Object)
	if !ok {
		return errors.New("objet invalide")
	}
	// Mise à jour des données de l'objet courant avec celles du nouvel objet
	*o = *newO
	return nil
}
func (o *Object) Remove(ctx context.Context) error {
	params := url.Values{}
	params.Set("key", o.fs.opt.APIKey)
	params.Set("file_code", o.code)
	opts := rest.Opts{
		Method:     "GET",
		Path:       "/file/delete",
		Parameters: params,
	}
	var resp *http.Response
	var delResp SimpleResponse
	err := o.fs.pacer.Call(func() (bool, error) {
		resp, err = o.fs.srv.CallJSON(ctx, &opts, nil, &delResp)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return err
	}
	if delResp.Status != 200 {
		return errors.New(delResp.Msg)
	}
	return nil
}
func contains(arr []int, code int) bool {
	for _, c := range arr {
		if c == code {
			return true
		}
	}
	return false
}
// randomString génère une chaîne aléatoire de n caractères.
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	// Pour simplifier, utiliser l'horodatage (à améliorer pour plus d'aléa)
	for i := range b {
		b[i] = letters[int(time.Now().UnixNano())%len(letters)]
	}
	return string(b)
}
// Vérification que les interfaces sont bien satisfaites.
var (
	_ fs.Fs     = (*Fs)(nil)
	_ fs.Object = (*Object)(nil)
)
