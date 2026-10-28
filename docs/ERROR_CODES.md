# Error codes

Every error the gateway returns has this shape, and nothing else:

```json
{
  "code": "AUTH_002",
  "message": {
    "en": "Email or password is incorrect.",
    "fr": "Email ou mot de passe incorrect.",
    "ar": "البريد الإلكتروني أو كلمة المرور غير صحيحة."
  }
}
```

- Codes are `SERVICE_NNN`. `_000` is reserved for unexpected errors.
- Text lives only in `gateway/internal/apperr/apperr.go`. Handlers reference codes, never strings.
- Clients branch on `code` and show `message` in the reader's language. Raw database or Docker errors are logged server-side and never sent.
- `gateway/internal/apperr` tests fail if any code is missing a language or is not listed below.

## Client handling

| Code | What the console does |
|---|---|
| `AUTH_005` | Refreshes the session once (`POST /api/v1/auth/refresh`) and retries the request |
| `AUTH_004`, `AUTH_006`, `AUTH_012` | Sends the user to sign in |
| `AUTH_002`, `AUTH_003`, `AUTH_009` | Shows the message under the matching form field |
| anything else | Shows the message in a toast or inline error |

## All codes

| Code | HTTP | EN | FR | AR |
|---|---|---|---|---|
| `APP_001` | 404 | App not found. | Application introuvable. | التطبيق غير موجود. |
| `APP_002` | 400 | App name is required. | Le nom de l'application est requis. | اسم التطبيق مطلوب. |
| `APP_003` | 501 | Deploying from a git repository is not available yet. Use the Client Tracker template. | Le déploiement depuis un dépôt git n'est pas encore disponible. Utilisez le modèle Client Tracker. | النشر من مستودع git غير متاح بعد. استخدم قالب Client Tracker. |
| `APP_004` | 502 | The app could not be started. | L'application n'a pas pu démarrer. | تعذر تشغيل التطبيق. |
| `APP_005` | 502 | The app started but did not pass its health check. | L'application a démarré mais n'a pas passé le contrôle de santé. | بدأ التطبيق لكنه لم يجتز فحص السلامة. |
| `AUTH_001` | 404 | No account exists with this email. Ask them to sign up first. | Aucun compte n'existe avec cet email. Demandez-lui de s'inscrire d'abord. | لا يوجد حساب بهذا البريد الإلكتروني. اطلب منه التسجيل أولاً. |
| `AUTH_002` | 401 | Email or password is incorrect. | Email ou mot de passe incorrect. | البريد الإلكتروني أو كلمة المرور غير صحيحة. |
| `AUTH_003` | 409 | An account with this email already exists. | Un compte existe déjà avec cet email. | يوجد حساب بهذا البريد الإلكتروني بالفعل. |
| `AUTH_004` | 401 | Your session is not valid. Please sign in again. | Votre session n'est pas valide. Veuillez vous reconnecter. | جلستك غير صالحة. يرجى تسجيل الدخول مرة أخرى. |
| `AUTH_005` | 401 | Your session has expired. | Votre session a expiré. | انتهت صلاحية جلستك. |
| `AUTH_006` | 401 | Please sign in to continue. | Veuillez vous connecter pour continuer. | يرجى تسجيل الدخول للمتابعة. |
| `AUTH_007` | 403 | Your role does not allow this action. | Votre rôle ne permet pas cette action. | دورك لا يسمح بهذا الإجراء. |
| `AUTH_008` | 403 | You are not a member of this workspace. | Vous n'êtes pas membre de cet espace de travail. | أنت لست عضوًا في مساحة العمل هذه. |
| `AUTH_009` | 400 | Password must be at least 8 characters. | Le mot de passe doit contenir au moins 8 caractères. | يجب أن تتكون كلمة المرور من 8 أحرف على الأقل. |
| `AUTH_010` | 400 | Email, password and workspace name are required. | Email, mot de passe et nom de l'espace de travail sont requis. | البريد الإلكتروني وكلمة المرور واسم مساحة العمل مطلوبة. |
| `AUTH_011` | 400 | Role must be owner, employee or accountant. | Le rôle doit être owner, employee ou accountant. | يجب أن يكون الدور owner أو employee أو accountant. |
| `AUTH_012` | 401 | Your session has ended. Please sign in again. | Votre session est terminée. Veuillez vous reconnecter. | انتهت جلستك. يرجى تسجيل الدخول مرة أخرى. |
| `CLIENT_001` | 404 | Client not found. | Client introuvable. | العميل غير موجود. |
| `CLIENT_002` | 400 | Client name is required. | Le nom du client est requis. | اسم العميل مطلوب. |
| `COMPANY_001` | 400 | Workspace name is required. | Le nom de l'espace de travail est requis. | اسم مساحة العمل مطلوب. |
| `GATEWAY_000` | 500 | Something went wrong on our side. Please try again. | Une erreur interne est survenue. Veuillez réessayer. | حدث خطأ داخلي. يرجى المحاولة مرة أخرى. |
| `GATEWAY_001` | 500 | The database could not complete this request. | La base de données n'a pas pu traiter cette requête. | تعذر على قاعدة البيانات إتمام هذا الطلب. |
| `GATEWAY_002` | 404 | This endpoint does not exist. | Ce point d'accès n'existe pas. | نقطة الوصول هذه غير موجودة. |
| `GATEWAY_003` | 405 | This method is not allowed here. | Cette méthode n'est pas autorisée ici. | هذه الطريقة غير مسموح بها هنا. |
| `GATEWAY_004` | 400 | The request body is not valid JSON. | Le corps de la requête n'est pas un JSON valide. | محتوى الطلب ليس JSON صالحًا. |
| `GATEWAY_005` | 503 | The app runtime is not available right now. | L'environnement d'exécution des apps est indisponible. | بيئة تشغيل التطبيقات غير متاحة حاليًا. |
| `GATEWAY_006` | 403 | Requests from this origin are not allowed. | Les requêtes depuis cette origine ne sont pas autorisées. | الطلبات من هذا المصدر غير مسموح بها. |
| `HEAL_001` | 404 | Proposal not found or already decided. | Proposition introuvable ou déjà traitée. | الاقتراح غير موجود أو تم البت فيه بالفعل. |
| `HEAL_002` | 409 | Approve this proposal before applying it. | Approuvez cette proposition avant de l'appliquer. | وافق على هذا الاقتراح قبل تطبيقه. |
| `HEAL_003` | 409 | This proposal was already applied. | Cette proposition a déjà été appliquée. | تم تطبيق هذا الاقتراح بالفعل. |
| `HEAL_004` | 400 | This fix is preview-only and cannot be applied automatically. | Ce correctif est en aperçu uniquement et ne peut pas être appliqué automatiquement. | هذا الإصلاح للمعاينة فقط ولا يمكن تطبيقه تلقائيًا. |
| `HEAL_005` | 500 | The fix could not be applied. | Le correctif n'a pas pu être appliqué. | تعذر تطبيق الإصلاح. |
| `SHARE_001` | 403 | This share link has expired or was revoked. | Ce lien de partage a expiré ou a été révoqué. | انتهت صلاحية رابط المشاركة هذا أو تم إلغاؤه. |
| `SHARE_002` | 400 | Choose a client to share. | Choisissez un client à partager. | اختر عميلاً للمشاركة. |
| `SHARE_003` | 404 | The shared record no longer exists. | L'enregistrement partagé n'existe plus. | السجل المشترك لم يعد موجودًا. |
| `SHARE_004` | 404 | Share not found. | Partage introuvable. | المشاركة غير موجودة. |
| `TENANT_001` | 403 | This record belongs to another workspace. | Cet enregistrement appartient à un autre espace de travail. | هذا السجل يخص مساحة عمل أخرى. |
