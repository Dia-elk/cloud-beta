// Package apperr is the gateway's single error-code registry. Every
// error sent to a client is {"code": "SERVICE_NNN", "message": {fr, ar, en}}.
// Handlers only ever reference a Code; human-readable text lives here
// and nowhere else. Codes are documented in docs/ERROR_CODES.md.
package apperr

import "net/http"

type Code string

type Def struct {
	Status int
	EN     string
	FR     string
	AR     string
}

const (
	GatewayInternal      Code = "GATEWAY_000"
	GatewayDatabase      Code = "GATEWAY_001"
	GatewayNotFound      Code = "GATEWAY_002"
	GatewayMethod        Code = "GATEWAY_003"
	GatewayBadBody       Code = "GATEWAY_004"
	GatewayDockerDown    Code = "GATEWAY_005"
	GatewayOrigin        Code = "GATEWAY_006"
	AuthUserNotFound     Code = "AUTH_001"
	AuthInvalidCreds     Code = "AUTH_002"
	AuthEmailExists      Code = "AUTH_003"
	AuthInvalidToken     Code = "AUTH_004"
	AuthTokenExpired     Code = "AUTH_005"
	AuthMissingToken     Code = "AUTH_006"
	AuthForbidden        Code = "AUTH_007"
	AuthNoMembership     Code = "AUTH_008"
	AuthPasswordShort    Code = "AUTH_009"
	AuthFieldsRequired   Code = "AUTH_010"
	AuthInvalidRole      Code = "AUTH_011"
	AuthRefreshInvalid   Code = "AUTH_012"
	TenantCrossDenied    Code = "TENANT_001"
	CompanyNameRequired  Code = "COMPANY_001"
	AppNotFound          Code = "APP_001"
	AppNameRequired      Code = "APP_002"
	AppGitDeployDisabled Code = "APP_003"
	AppDeployFailed      Code = "APP_004"
	AppUnhealthy         Code = "APP_005"
	ClientNotFound       Code = "CLIENT_001"
	ClientNameRequired   Code = "CLIENT_002"
	ShareInvalid         Code = "SHARE_001"
	ShareRowRequired     Code = "SHARE_002"
	ShareRowNotFound     Code = "SHARE_003"
	ShareNotFound        Code = "SHARE_004"
	HealNotFound         Code = "HEAL_001"
	HealApproveFirst     Code = "HEAL_002"
	HealAlreadyApplied   Code = "HEAL_003"
	HealNotApplyable     Code = "HEAL_004"
	HealApplyFailed      Code = "HEAL_005"
)

var registry = map[Code]Def{
	GatewayInternal:      {http.StatusInternalServerError, "Something went wrong on our side. Please try again.", "Une erreur interne est survenue. Veuillez réessayer.", "حدث خطأ داخلي. يرجى المحاولة مرة أخرى."},
	GatewayDatabase:      {http.StatusInternalServerError, "The database could not complete this request.", "La base de données n'a pas pu traiter cette requête.", "تعذر على قاعدة البيانات إتمام هذا الطلب."},
	GatewayNotFound:      {http.StatusNotFound, "This endpoint does not exist.", "Ce point d'accès n'existe pas.", "نقطة الوصول هذه غير موجودة."},
	GatewayMethod:        {http.StatusMethodNotAllowed, "This method is not allowed here.", "Cette méthode n'est pas autorisée ici.", "هذه الطريقة غير مسموح بها هنا."},
	GatewayBadBody:       {http.StatusBadRequest, "The request body is not valid JSON.", "Le corps de la requête n'est pas un JSON valide.", "محتوى الطلب ليس JSON صالحًا."},
	GatewayDockerDown:    {http.StatusServiceUnavailable, "The app runtime is not available right now.", "L'environnement d'exécution des apps est indisponible.", "بيئة تشغيل التطبيقات غير متاحة حاليًا."},
	GatewayOrigin:        {http.StatusForbidden, "Requests from this origin are not allowed.", "Les requêtes depuis cette origine ne sont pas autorisées.", "الطلبات من هذا المصدر غير مسموح بها."},
	AuthUserNotFound:     {http.StatusNotFound, "No account exists with this email. Ask them to sign up first.", "Aucun compte n'existe avec cet email. Demandez-lui de s'inscrire d'abord.", "لا يوجد حساب بهذا البريد الإلكتروني. اطلب منه التسجيل أولاً."},
	AuthInvalidCreds:     {http.StatusUnauthorized, "Email or password is incorrect.", "Email ou mot de passe incorrect.", "البريد الإلكتروني أو كلمة المرور غير صحيحة."},
	AuthEmailExists:      {http.StatusConflict, "An account with this email already exists.", "Un compte existe déjà avec cet email.", "يوجد حساب بهذا البريد الإلكتروني بالفعل."},
	AuthInvalidToken:     {http.StatusUnauthorized, "Your session is not valid. Please sign in again.", "Votre session n'est pas valide. Veuillez vous reconnecter.", "جلستك غير صالحة. يرجى تسجيل الدخول مرة أخرى."},
	AuthTokenExpired:     {http.StatusUnauthorized, "Your session has expired.", "Votre session a expiré.", "انتهت صلاحية جلستك."},
	AuthMissingToken:     {http.StatusUnauthorized, "Please sign in to continue.", "Veuillez vous connecter pour continuer.", "يرجى تسجيل الدخول للمتابعة."},
	AuthForbidden:        {http.StatusForbidden, "Your role does not allow this action.", "Votre rôle ne permet pas cette action.", "دورك لا يسمح بهذا الإجراء."},
	AuthNoMembership:     {http.StatusForbidden, "You are not a member of this workspace.", "Vous n'êtes pas membre de cet espace de travail.", "أنت لست عضوًا في مساحة العمل هذه."},
	AuthPasswordShort:    {http.StatusBadRequest, "Password must be at least 8 characters.", "Le mot de passe doit contenir au moins 8 caractères.", "يجب أن تتكون كلمة المرور من 8 أحرف على الأقل."},
	AuthFieldsRequired:   {http.StatusBadRequest, "Email, password and workspace name are required.", "Email, mot de passe et nom de l'espace de travail sont requis.", "البريد الإلكتروني وكلمة المرور واسم مساحة العمل مطلوبة."},
	AuthInvalidRole:      {http.StatusBadRequest, "Role must be owner, employee or accountant.", "Le rôle doit être owner, employee ou accountant.", "يجب أن يكون الدور owner أو employee أو accountant."},
	AuthRefreshInvalid:   {http.StatusUnauthorized, "Your session has ended. Please sign in again.", "Votre session est terminée. Veuillez vous reconnecter.", "انتهت جلستك. يرجى تسجيل الدخول مرة أخرى."},
	TenantCrossDenied:    {http.StatusForbidden, "This record belongs to another workspace.", "Cet enregistrement appartient à un autre espace de travail.", "هذا السجل يخص مساحة عمل أخرى."},
	CompanyNameRequired:  {http.StatusBadRequest, "Workspace name is required.", "Le nom de l'espace de travail est requis.", "اسم مساحة العمل مطلوب."},
	AppNotFound:          {http.StatusNotFound, "App not found.", "Application introuvable.", "التطبيق غير موجود."},
	AppNameRequired:      {http.StatusBadRequest, "App name is required.", "Le nom de l'application est requis.", "اسم التطبيق مطلوب."},
	AppGitDeployDisabled: {http.StatusNotImplemented, "Deploying from a git repository is not available yet. Use the Client Tracker template.", "Le déploiement depuis un dépôt git n'est pas encore disponible. Utilisez le modèle Client Tracker.", "النشر من مستودع git غير متاح بعد. استخدم قالب Client Tracker."},
	AppDeployFailed:      {http.StatusBadGateway, "The app could not be started.", "L'application n'a pas pu démarrer.", "تعذر تشغيل التطبيق."},
	AppUnhealthy:         {http.StatusBadGateway, "The app started but did not pass its health check.", "L'application a démarré mais n'a pas passé le contrôle de santé.", "بدأ التطبيق لكنه لم يجتز فحص السلامة."},
	ClientNotFound:       {http.StatusNotFound, "Client not found.", "Client introuvable.", "العميل غير موجود."},
	ClientNameRequired:   {http.StatusBadRequest, "Client name is required.", "Le nom du client est requis.", "اسم العميل مطلوب."},
	ShareInvalid:         {http.StatusForbidden, "This share link has expired or was revoked.", "Ce lien de partage a expiré ou a été révoqué.", "انتهت صلاحية رابط المشاركة هذا أو تم إلغاؤه."},
	ShareRowRequired:     {http.StatusBadRequest, "Choose a client to share.", "Choisissez un client à partager.", "اختر عميلاً للمشاركة."},
	ShareRowNotFound:     {http.StatusNotFound, "The shared record no longer exists.", "L'enregistrement partagé n'existe plus.", "السجل المشترك لم يعد موجودًا."},
	ShareNotFound:        {http.StatusNotFound, "Share not found.", "Partage introuvable.", "المشاركة غير موجودة."},
	HealNotFound:         {http.StatusNotFound, "Proposal not found or already decided.", "Proposition introuvable ou déjà traitée.", "الاقتراح غير موجود أو تم البت فيه بالفعل."},
	HealApproveFirst:     {http.StatusConflict, "Approve this proposal before applying it.", "Approuvez cette proposition avant de l'appliquer.", "وافق على هذا الاقتراح قبل تطبيقه."},
	HealAlreadyApplied:   {http.StatusConflict, "This proposal was already applied.", "Cette proposition a déjà été appliquée.", "تم تطبيق هذا الاقتراح بالفعل."},
	HealNotApplyable:     {http.StatusBadRequest, "This fix is preview-only and cannot be applied automatically.", "Ce correctif est en aperçu uniquement et ne peut pas être appliqué automatiquement.", "هذا الإصلاح للمعاينة فقط ولا يمكن تطبيقه تلقائيًا."},
	HealApplyFailed:      {http.StatusInternalServerError, "The fix could not be applied.", "Le correctif n'a pas pu être appliqué.", "تعذر تطبيق الإصلاح."},
}

// Body is the wire shape of every error response.
type Body struct {
	Code    Code              `json:"code"`
	Message map[string]string `json:"message"`
}

// Lookup returns the HTTP status and response body for a code. An
// unregistered code falls back to GATEWAY_000 so a typo can never leak
// as an empty or 200 response.
func Lookup(c Code) (int, Body) {
	d, ok := registry[c]
	if !ok {
		c, d = GatewayInternal, registry[GatewayInternal]
	}
	return d.Status, Body{Code: c, Message: map[string]string{"en": d.EN, "fr": d.FR, "ar": d.AR}}
}

// All returns every registered code, for documentation and tests.
func All() map[Code]Def { return registry }
