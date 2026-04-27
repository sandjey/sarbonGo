package resp

import (
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"

	"sarbonNew/internal/util"
)

// MsgAllLangs returns localized strings for apiMessages[key] in all five API languages (en, ru, uz, tr, zh).
func MsgAllLangs(key string) map[string]string {
	langs := []string{"en", "ru", "uz", "tr", "zh"}
	out := make(map[string]string, len(langs))
	for _, l := range langs {
		out[l] = Msg(key, l)
	}
	return out
}

// Envelope is the unified response structure for ALL API endpoints.
type Envelope struct {
	Status      string `json:"status"`      // success | error
	Code        int    `json:"code"`        // usually HTTP status code
	Description string `json:"description"` // human readable
	Data        any    `json:"data"`        // object | array | null
}

func Success(c *gin.Context, httpCode int, description string, data any) {
	c.JSON(httpCode, Envelope{
		Status:      "success",
		Code:        httpCode,
		Description: description,
		Data:        localizeAnyTimeToTashkent(data),
	})
}

func OK(c *gin.Context, data any) {
	Success(c, http.StatusOK, "ok", data)
}

func Error(c *gin.Context, httpCode int, description string) {
	c.JSON(httpCode, Envelope{
		Status:      "error",
		Code:        httpCode,
		Description: description,
		Data:        nil,
	})
}

// ErrorWithData sends error response with optional data (e.g. limit, current count).
func ErrorWithData(c *gin.Context, httpCode int, description string, data any) {
	c.JSON(httpCode, Envelope{
		Status:      "error",
		Code:        httpCode,
		Description: description,
		Data:        localizeAnyTimeToTashkent(data),
	})
}

func localizeAnyTimeToTashkent(v any) any {
	if v == nil {
		return nil
	}
	return localizeValue(reflect.ValueOf(v)).Interface()
}

func localizeValue(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	if !v.CanInterface() {
		return v
	}

	// Fast-path for time.Time.
	if v.Type() == reflect.TypeOf(time.Time{}) {
		t := v.Interface().(time.Time)
		return reflect.ValueOf(util.InTashkent(t))
	}

	// Fast-path for *time.Time.
	if v.Type() == reflect.TypeOf(&time.Time{}) {
		if v.IsNil() {
			return v
		}
		t := v.Elem().Interface().(time.Time)
		tt := util.InTashkent(t)
		p := reflect.New(reflect.TypeOf(time.Time{}))
		p.Elem().Set(reflect.ValueOf(tt))
		return p
	}

	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		elem := localizeValue(v.Elem())
		p := reflect.New(v.Type().Elem())
		if elem.IsValid() && elem.Type().AssignableTo(v.Type().Elem()) {
			p.Elem().Set(elem)
		} else {
			p.Elem().Set(v.Elem())
		}
		return p
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		ev := localizeValue(v.Elem())
		return ev
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v) // preserve all fields first
		for i := 0; i < v.NumField(); i++ {
			dst := out.Field(i)
			if !dst.CanSet() {
				continue
			}
			lv := localizeValue(v.Field(i))
			if lv.IsValid() && lv.Type().AssignableTo(dst.Type()) {
				dst.Set(lv)
			}
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			lv := localizeValue(v.Index(i))
			if lv.IsValid() && lv.Type().AssignableTo(v.Type().Elem()) {
				out.Index(i).Set(lv)
			} else {
				out.Index(i).Set(v.Index(i))
			}
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			lv := localizeValue(v.Index(i))
			if lv.IsValid() && lv.Type().AssignableTo(v.Type().Elem()) {
				out.Index(i).Set(lv)
			} else {
				out.Index(i).Set(v.Index(i))
			}
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			k := iter.Key()
			mv := iter.Value()
			lv := localizeValue(mv)
			if lv.IsValid() && lv.Type().AssignableTo(v.Type().Elem()) {
				out.SetMapIndex(k, lv)
			} else {
				out.SetMapIndex(k, mv)
			}
		}
		return out
	default:
		return v
	}
}

// Lang returns X-Language from request (ru, uz, en, tr, zh). Default "en".
func Lang(c *gin.Context) string {
	return LangFromContext(c)
}

// OKLang sends success 200 with description by message key and X-Language. status stays "success" (English).
func OKLang(c *gin.Context, messageKey string, data any) {
	desc := Msg(messageKey, Lang(c))
	Success(c, http.StatusOK, desc, data)
}

// SuccessLang sends success with code and localized description by key.
func SuccessLang(c *gin.Context, httpCode int, messageKey string, data any) {
	desc := Msg(messageKey, Lang(c))
	Success(c, httpCode, desc, data)
}

// ErrorLang sends error response with description by message key and X-Language. status stays "error" (English).
func ErrorLang(c *gin.Context, httpCode int, messageKey string) {
	desc := Msg(messageKey, Lang(c))
	Error(c, httpCode, desc)
}

// ErrorWithDataLang sends localized error response with optional data (e.g. fields map).
func ErrorWithDataLang(c *gin.Context, httpCode int, messageKey string, data any) {
	desc := Msg(messageKey, Lang(c))
	ErrorWithData(c, httpCode, desc, data)
}

