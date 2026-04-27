--
-- PostgreSQL database dump
--

\restrict XhfTMsZsakoI0kcp5V1zCnhkFDp6rw5PYcOJbIRnulj0iN5v9ZfcDGPY8GDHPa6

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


--
-- Name: uuid-ossp; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public;


--
-- Name: EXTENSION "uuid-ossp"; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION "uuid-ossp" IS 'generate universally unique identifiers (UUIDs)';


--
-- Name: call_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.call_status AS ENUM (
    'RINGING',
    'ACTIVE',
    'ENDED',
    'DECLINED',
    'MISSED',
    'CANCELLED',
    'FAILED'
);


--
-- Name: admins_hash_password_trigger(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admins_hash_password_trigger() RETURNS trigger
    LANGUAGE plpgsql
    AS $_$

BEGIN

  -- If password looks like bcrypt ($2a$, $2b$, etc.), leave as is (from API/cmd/admin).

  IF NEW.password IS NOT NULL AND length(trim(NEW.password)) > 0 AND NEW.password NOT LIKE '$2%' THEN

    NEW.password := crypt(NEW.password, gen_salt('bf'));

  END IF;

  -- On update, empty string usually means "don't change"; restore old hash.

  IF TG_OP = 'UPDATE' AND (NEW.password IS NULL OR trim(NEW.password) = '') THEN

    NEW.password := OLD.password;

  END IF;

  RETURN NEW;

END;

$_$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: admins; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admins (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    login character varying NOT NULL,
    password character varying NOT NULL,
    name character varying NOT NULL,
    status character varying DEFAULT 'active'::character varying NOT NULL,
    type character varying DEFAULT 'creator'::character varying NOT NULL,
    CONSTRAINT admins_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'inactive'::character varying, 'blocked'::character varying])::text[])))
);


--
-- Name: analytics_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.analytics_events (
    event_id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    event_name character varying(64) NOT NULL,
    event_time_utc timestamp with time zone DEFAULT now() NOT NULL,
    session_id character varying(128),
    user_id uuid,
    role character varying(32) NOT NULL,
    entity_type character varying(64),
    entity_id uuid,
    actor_id uuid,
    device_type character varying(32),
    platform text,
    ip_hash character varying(128),
    geo_city character varying(128),
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL
);


--
-- Name: app_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.app_roles (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying(50) NOT NULL,
    description text
);


--
-- Name: archived_cargo; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.archived_cargo (
    id uuid NOT NULL,
    snapshot jsonb NOT NULL,
    archived_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: archived_trips; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.archived_trips (
    id uuid NOT NULL,
    cargo_id uuid NOT NULL,
    offer_id uuid NOT NULL,
    driver_id uuid,
    status character varying(50) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    archived_at timestamp with time zone DEFAULT now() NOT NULL,
    cancel_reason text,
    cancelled_by_role character varying(20),
    agreed_price numeric(18,2) DEFAULT 0 NOT NULL,
    agreed_currency character varying(3) DEFAULT 'UZS'::character varying NOT NULL
);


--
-- Name: audit_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_log (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    user_id uuid,
    company_id uuid,
    action character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    entity_id uuid NOT NULL,
    old_data jsonb,
    new_data jsonb,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: call_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.call_events (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    call_id uuid NOT NULL,
    actor_id uuid,
    event_type character varying(50) NOT NULL,
    payload jsonb,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: calls; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.calls (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    conversation_id uuid,
    caller_id uuid NOT NULL,
    callee_id uuid NOT NULL,
    status public.call_status DEFAULT 'RINGING'::public.call_status NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    started_at timestamp without time zone,
    ended_at timestamp without time zone,
    ended_by uuid,
    ended_reason character varying(50),
    client_request_id character varying(64),
    CONSTRAINT calls_not_same_user CHECK ((caller_id <> callee_id))
);


--
-- Name: cargo; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    weight double precision NOT NULL,
    volume double precision NOT NULL,
    ready_enabled boolean DEFAULT false NOT NULL,
    ready_at timestamp without time zone,
    load_comment character varying,
    truck_type character varying NOT NULL,
    temp_min double precision,
    temp_max double precision,
    adr_enabled boolean DEFAULT false NOT NULL,
    adr_class character varying,
    loading_types text[],
    requirements text[],
    shipment_type character varying,
    belts_count integer,
    documents jsonb,
    contact_name character varying,
    contact_phone character varying,
    status character varying DEFAULT 'created'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone,
    created_by_type character varying,
    created_by_id uuid,
    company_id uuid,
    moderation_rejection_reason text,
    name character varying(255),
    cargo_type_id uuid,
    capacity_required double precision,
    packaging character varying(500),
    dimensions character varying(500),
    photo_urls text[],
    power_plate_type character varying,
    trailer_plate_type character varying,
    vehicles_amount integer,
    vehicles_left integer NOT NULL,
    way_points jsonb,
    packaging_amount integer,
    is_two_drivers_required boolean DEFAULT false NOT NULL,
    unloading_types text[],
    prev_status text,
    CONSTRAINT cargo_created_by_type_check CHECK (((created_by_type IS NULL) OR ((created_by_type)::text = ANY ((ARRAY['ADMIN'::character varying, 'DISPATCHER'::character varying, 'COMPANY'::character varying])::text[])))),
    CONSTRAINT cargo_prev_status_check CHECK (((prev_status IS NULL) OR (prev_status = ANY (ARRAY['SEARCHING_ALL'::text, 'SEARCHING_COMPANY'::text])))),
    CONSTRAINT cargo_status_check CHECK (((status)::text = ANY ((ARRAY['PENDING_MODERATION'::character varying, 'SEARCHING_ALL'::character varying, 'SEARCHING_COMPANY'::character varying, 'PROCESSING'::character varying, 'COMPLETED'::character varying, 'CANCELLED'::character varying])::text[]))),
    CONSTRAINT cargo_weight_check CHECK ((weight > (0)::double precision))
);


--
-- Name: cargo_driver_recommendations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_driver_recommendations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    driver_id uuid NOT NULL,
    invited_by_dispatcher_id uuid NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: cargo_drivers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_drivers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    driver_id uuid NOT NULL,
    status character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT cargo_drivers_status_chk CHECK (((status)::text = ANY ((ARRAY['ACTIVE'::character varying, 'COMPLETED'::character varying, 'CANCELLED'::character varying, 'REMOVED'::character varying])::text[])))
);


--
-- Name: cargo_manager_dm_offers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_manager_dm_offers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    cargo_manager_id uuid NOT NULL,
    driver_manager_id uuid NOT NULL,
    driver_id uuid,
    offer_id uuid,
    price double precision NOT NULL,
    currency character varying NOT NULL,
    comment character varying,
    status character varying DEFAULT 'PENDING'::character varying NOT NULL,
    rejection_reason text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT cargo_manager_dm_offers_status_check CHECK (((status)::text = ANY ((ARRAY['PENDING'::character varying, 'ACCEPTED'::character varying, 'REJECTED'::character varying, 'CANCELED'::character varying])::text[])))
);


--
-- Name: cargo_pending_photos; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_pending_photos (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    mime character varying(128) NOT NULL,
    size_bytes bigint NOT NULL,
    path character varying(1024) NOT NULL,
    uploader_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: cargo_photos; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_photos (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    uploader_id uuid,
    mime character varying(128) NOT NULL,
    size_bytes bigint NOT NULL,
    path character varying(1024) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: cargo_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_stats (
    day_utc date NOT NULL,
    role character varying(32) NOT NULL,
    user_id uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    created_count bigint DEFAULT 0 NOT NULL,
    completed_count bigint DEFAULT 0 NOT NULL,
    cancelled_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: cargo_types; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_types (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    code character varying(128) NOT NULL,
    name_ru character varying(255) NOT NULL,
    name_uz character varying(255) NOT NULL,
    name_en character varying(255) NOT NULL,
    name_tr character varying(255) NOT NULL,
    name_zh character varying(255) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: chat_attachments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_attachments (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    message_id uuid,
    conversation_id uuid NOT NULL,
    uploader_id uuid NOT NULL,
    kind character varying(20) NOT NULL,
    mime character varying(128) NOT NULL,
    size_bytes bigint NOT NULL,
    path character varying(1024) NOT NULL,
    thumb_path character varying(1024),
    width integer,
    height integer,
    duration_ms integer,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    media_file_id uuid,
    thumb_media_file_id uuid
);


--
-- Name: chat_conversation_reads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_conversation_reads (
    conversation_id uuid NOT NULL,
    user_id uuid NOT NULL,
    last_read_at timestamp with time zone DEFAULT '1970-01-01 06:00:00+06'::timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: chat_conversations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_conversations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    user_a_id uuid NOT NULL,
    user_b_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT chat_conv_ordered CHECK ((user_a_id < user_b_id))
);


--
-- Name: chat_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_messages (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    conversation_id uuid NOT NULL,
    sender_id uuid NOT NULL,
    body text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone,
    type character varying(20) DEFAULT 'TEXT'::character varying NOT NULL,
    payload jsonb,
    delivered_at timestamp with time zone
);


--
-- Name: chat_source_hashes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_source_hashes (
    source_hash text NOT NULL,
    media_file_id uuid NOT NULL,
    thumb_media_file_id uuid,
    kind text NOT NULL,
    mime text NOT NULL,
    size_bytes bigint NOT NULL,
    duration_ms integer,
    width integer,
    height integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: cities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cities (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    code character varying(20) NOT NULL,
    name_ru character varying(255) NOT NULL,
    name_en character varying(255),
    country_code character varying(3) NOT NULL,
    lat double precision,
    lng double precision,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: companies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.companies (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying NOT NULL,
    inn character varying,
    address character varying,
    phone character varying,
    email character varying,
    website character varying,
    license_number character varying,
    status character varying DEFAULT 'pending'::character varying NOT NULL,
    rating double precision,
    completed_orders integer DEFAULT 0 NOT NULL,
    cancelled_orders integer DEFAULT 0 NOT NULL,
    total_revenue numeric(18,2) DEFAULT 0 NOT NULL,
    created_by uuid,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone,
    max_cargo integer DEFAULT 0 NOT NULL,
    max_dispatchers integer DEFAULT 0 NOT NULL,
    max_managers integer DEFAULT 0 NOT NULL,
    max_top_dispatchers integer DEFAULT 0 NOT NULL,
    max_top_managers integer DEFAULT 0 NOT NULL,
    max_vehicles integer DEFAULT 0 NOT NULL,
    max_drivers integer DEFAULT 0 NOT NULL,
    owner_id uuid,
    company_type character varying(20),
    auto_approve_limit numeric(10,2),
    owner_dispatcher_id uuid,
    CONSTRAINT companies_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'inactive'::character varying, 'blocked'::character varying, 'pending'::character varying])::text[]))),
    CONSTRAINT companies_type_check CHECK (((company_type IS NULL) OR ((company_type)::text = ''::text) OR ((company_type)::text = ANY ((ARRAY['CargoOwner'::character varying, 'Carrier'::character varying, 'Expeditor'::character varying])::text[]))))
);


--
-- Name: company_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.company_users (
    id uuid DEFAULT public.uuid_generate_v4() CONSTRAINT app_users_id_not_null NOT NULL,
    phone character varying(20),
    password_hash character varying(255) CONSTRAINT app_users_password_hash_not_null NOT NULL,
    first_name character varying(100),
    last_name character varying(100),
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    company_id uuid,
    role character varying(50)
);


--
-- Name: deleted_drivers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.deleted_drivers (
    id uuid DEFAULT public.uuid_generate_v4() CONSTRAINT drivers_id_not_null NOT NULL,
    phone character varying CONSTRAINT drivers_phone_not_null NOT NULL,
    created_at timestamp without time zone DEFAULT now() CONSTRAINT drivers_created_at_not_null NOT NULL,
    updated_at timestamp without time zone DEFAULT now() CONSTRAINT drivers_updated_at_not_null NOT NULL,
    last_online_at timestamp without time zone,
    latitude double precision,
    longitude double precision,
    push_token character varying,
    registration_step character varying,
    registration_status character varying,
    name character varying,
    driver_type character varying,
    rating double precision,
    work_status character varying,
    freelancer_id uuid,
    company_id uuid,
    account_status character varying,
    driver_passport_series character varying,
    driver_passport_number character varying,
    driver_pinfl character varying,
    driver_scan_status boolean,
    driver_owner boolean,
    kyc_status character varying,
    photo_data bytea,
    photo_content_type character varying(50)
);


--
-- Name: deleted_freelance_dispatchers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.deleted_freelance_dispatchers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying,
    phone character varying,
    password character varying,
    passport_series character varying,
    passport_number character varying,
    pinfl character varying,
    cargo_id uuid,
    driver_id uuid,
    rating double precision,
    work_status character varying,
    account_status character varying,
    photo_path character varying,
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    deleted_at timestamp without time zone,
    last_online_at timestamp without time zone,
    photo_data bytea,
    photo_content_type character varying(50),
    manager_role character varying(32)
);


--
-- Name: dispatcher_company_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dispatcher_company_roles (
    dispatcher_id uuid NOT NULL,
    company_id uuid NOT NULL,
    role character varying(50) NOT NULL,
    invited_by uuid,
    accepted_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: dispatcher_invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dispatcher_invitations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    token character varying(64) NOT NULL,
    company_id uuid NOT NULL,
    role character varying(50) NOT NULL,
    phone character varying(20) NOT NULL,
    invited_by uuid NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: driver_cargo_favorites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_cargo_favorites (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    driver_id uuid NOT NULL,
    cargo_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: driver_dispatcher_favorites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_dispatcher_favorites (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    driver_id uuid NOT NULL,
    dispatcher_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: driver_invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_invitations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    token character varying(64) NOT NULL,
    company_id uuid,
    phone character varying(20) NOT NULL,
    invited_by uuid NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    invited_by_dispatcher_id uuid,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    responded_at timestamp with time zone,
    CONSTRAINT chk_driver_invitations_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'accepted'::character varying, 'declined'::character varying, 'cancelled'::character varying])::text[])))
);


--
-- Name: driver_manager_relations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_manager_relations (
    driver_id uuid NOT NULL,
    manager_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now()
);


--
-- Name: driver_powers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_powers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    driver_id uuid NOT NULL,
    power_plate_type character varying,
    power_plate_number character varying,
    power_tech_series character varying,
    power_tech_number character varying,
    power_owner_id character varying,
    power_owner_name character varying,
    power_scan_status boolean,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: driver_to_dispatcher_invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_to_dispatcher_invitations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    token character varying(64) NOT NULL,
    driver_id uuid NOT NULL,
    dispatcher_phone character varying(32) NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    responded_at timestamp with time zone,
    CONSTRAINT chk_d2d_invitations_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'accepted'::character varying, 'declined'::character varying, 'cancelled'::character varying])::text[])))
);


--
-- Name: driver_trailers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_trailers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    driver_id uuid NOT NULL,
    trailer_plate_type character varying,
    trailer_plate_number character varying,
    trailer_tech_series character varying,
    trailer_tech_number character varying,
    trailer_owner_id character varying,
    trailer_owner_name character varying,
    trailer_scan_status boolean,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: drivers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.drivers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    phone character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    last_online_at timestamp without time zone,
    latitude double precision,
    longitude double precision,
    push_token character varying,
    registration_step character varying,
    registration_status character varying,
    name character varying,
    driver_type character varying,
    rating double precision,
    work_status character varying,
    freelancer_id uuid,
    company_id uuid,
    account_status character varying,
    driver_passport_series character varying,
    driver_passport_number character varying,
    driver_pinfl character varying,
    driver_scan_status boolean,
    driver_owner boolean,
    kyc_status character varying,
    photo_data bytea,
    photo_content_type character varying(50)
);


--
-- Name: freelance_dispatcher_cargo_favorites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.freelance_dispatcher_cargo_favorites (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    dispatcher_id uuid NOT NULL,
    cargo_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: freelance_dispatcher_driver_favorites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.freelance_dispatcher_driver_favorites (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    dispatcher_id uuid NOT NULL,
    driver_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: freelance_dispatchers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.freelance_dispatchers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying NOT NULL,
    phone character varying NOT NULL,
    password character varying NOT NULL,
    passport_series character varying,
    passport_number character varying,
    pinfl character varying,
    cargo_id uuid,
    driver_id uuid,
    rating double precision,
    work_status character varying,
    account_status character varying,
    photo_path character varying,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone,
    last_online_at timestamp without time zone,
    photo_data bytea,
    photo_content_type character varying(50),
    manager_role character varying(32),
    push_token character varying,
    CONSTRAINT chk_freelance_dispatchers_manager_role CHECK (((manager_role IS NULL) OR ((manager_role)::text = ANY ((ARRAY['CARGO_MANAGER'::character varying, 'DRIVER_MANAGER'::character varying])::text[]))))
);


--
-- Name: geo_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.geo_stats (
    day_utc date NOT NULL,
    geo_city character varying(128) NOT NULL,
    role character varying(32) NOT NULL,
    user_id uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    event_count bigint DEFAULT 0 NOT NULL,
    login_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: goadmin_menu; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_menu (
    id integer NOT NULL,
    parent_id integer DEFAULT 0 NOT NULL,
    type smallint DEFAULT 0 NOT NULL,
    "order" integer DEFAULT 0 NOT NULL,
    title character varying(50) NOT NULL,
    icon character varying(50) NOT NULL,
    uri character varying(3000) DEFAULT ''::character varying NOT NULL,
    header character varying(150),
    plugin_name character varying(150) DEFAULT ''::character varying NOT NULL,
    uuid character varying(150),
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_menu_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_menu_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_menu_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_menu_id_seq OWNED BY public.goadmin_menu.id;


--
-- Name: goadmin_operation_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_operation_log (
    id integer NOT NULL,
    user_id integer NOT NULL,
    path character varying(255) NOT NULL,
    method character varying(10) NOT NULL,
    ip character varying(15) NOT NULL,
    input text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_operation_log_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_operation_log_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_operation_log_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_operation_log_id_seq OWNED BY public.goadmin_operation_log.id;


--
-- Name: goadmin_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_permissions (
    id integer NOT NULL,
    name character varying(50) NOT NULL,
    slug character varying(50) NOT NULL,
    http_method character varying(255),
    http_path text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_permissions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_permissions_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_permissions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_permissions_id_seq OWNED BY public.goadmin_permissions.id;


--
-- Name: goadmin_role_menu; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_role_menu (
    role_id integer NOT NULL,
    menu_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_role_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_role_permissions (
    role_id integer NOT NULL,
    permission_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_role_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_role_users (
    role_id integer NOT NULL,
    user_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_roles (
    id integer NOT NULL,
    name character varying(50) NOT NULL,
    slug character varying(50) NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_roles_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_roles_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_roles_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_roles_id_seq OWNED BY public.goadmin_roles.id;


--
-- Name: goadmin_session; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_session (
    id integer NOT NULL,
    sid character varying(50) DEFAULT ''::character varying NOT NULL,
    "values" character varying(3000) NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_session_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_session_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_session_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_session_id_seq OWNED BY public.goadmin_session.id;


--
-- Name: goadmin_site; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_site (
    id integer NOT NULL,
    key character varying(100),
    value text,
    description character varying(3000),
    state smallint DEFAULT 0 NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_site_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_site_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_site_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_site_id_seq OWNED BY public.goadmin_site.id;


--
-- Name: goadmin_user_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_user_permissions (
    user_id integer NOT NULL,
    permission_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_users (
    id integer NOT NULL,
    username character varying(100) NOT NULL,
    password character varying(100) DEFAULT ''::character varying NOT NULL,
    name character varying(100) NOT NULL,
    avatar character varying(255),
    remember_token character varying(100),
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_users_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_users_id_seq OWNED BY public.goadmin_users.id;


--
-- Name: invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invitations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    token character varying(64) NOT NULL,
    company_id uuid NOT NULL,
    role_id uuid NOT NULL,
    email character varying(255) NOT NULL,
    invited_by uuid NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: media_files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_files (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    content_hash character varying(64) NOT NULL,
    kind character varying(20) NOT NULL,
    mime character varying(128) NOT NULL,
    size_bytes bigint NOT NULL,
    path character varying(1024) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: offer_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.offer_stats (
    day_utc date NOT NULL,
    role character varying(32) NOT NULL,
    user_id uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    created_count bigint DEFAULT 0 NOT NULL,
    accepted_count bigint DEFAULT 0 NOT NULL,
    rejected_count bigint DEFAULT 0 NOT NULL,
    canceled_count bigint DEFAULT 0 NOT NULL,
    waiting_driver_confirm_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: offers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.offers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    carrier_id uuid NOT NULL,
    price double precision NOT NULL,
    currency character varying NOT NULL,
    comment character varying,
    status character varying DEFAULT 'pending'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    rejection_reason text,
    proposed_by character varying(20) DEFAULT 'DRIVER'::character varying NOT NULL,
    proposed_by_id uuid,
    negotiation_dispatcher_id uuid,
    CONSTRAINT offers_proposed_by_check CHECK (((proposed_by)::text = ANY ((ARRAY['DRIVER'::character varying, 'DISPATCHER'::character varying, 'DRIVER_MANAGER'::character varying])::text[]))),
    CONSTRAINT offers_status_check CHECK (((status)::text = ANY ((ARRAY['PENDING'::character varying, 'ACCEPTED'::character varying, 'REJECTED'::character varying, 'WAITING_DRIVER_CONFIRM'::character varying, 'CANCELED'::character varying])::text[])))
);


--
-- Name: payments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payments (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    is_negotiable boolean DEFAULT false NOT NULL,
    price_request boolean DEFAULT false NOT NULL,
    total_amount double precision,
    total_currency character varying,
    with_prepayment boolean DEFAULT false NOT NULL,
    without_prepayment boolean DEFAULT true NOT NULL,
    prepayment_amount double precision,
    prepayment_currency character varying,
    prepayment_type character varying,
    remaining_amount double precision,
    remaining_currency character varying,
    remaining_type character varying,
    payment_note character varying(500),
    payment_terms_note text
);


--
-- Name: regions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.regions (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    code character varying(20) NOT NULL,
    name_ru character varying(255) NOT NULL,
    name_en character varying(255),
    country_code character varying(3) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: route_points; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_points (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    type character varying NOT NULL,
    address character varying NOT NULL,
    lat double precision NOT NULL,
    lng double precision NOT NULL,
    comment character varying,
    point_order integer NOT NULL,
    is_main_load boolean DEFAULT false NOT NULL,
    is_main_unload boolean DEFAULT false NOT NULL,
    city_code character varying(20),
    region_code character varying(20),
    orientir character varying(500),
    place_id character varying(255),
    point_at timestamp with time zone,
    country_code character varying(3),
    CONSTRAINT route_points_type_check CHECK (((type)::text = ANY ((ARRAY['LOAD'::character varying, 'UNLOAD'::character varying, 'CUSTOMS'::character varying, 'TRANSIT'::character varying])::text[])))
);


--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    session_id character varying(128) NOT NULL,
    user_id uuid NOT NULL,
    role character varying(32) NOT NULL,
    started_at_utc timestamp with time zone NOT NULL,
    ended_at_utc timestamp with time zone,
    last_seen_at_utc timestamp with time zone,
    duration_seconds bigint DEFAULT 0 NOT NULL,
    device_type character varying(32),
    platform text,
    ip_hash character varying(128),
    geo_city character varying(128),
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL
);


--
-- Name: trip_ratings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trip_ratings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    trip_id uuid NOT NULL,
    rater_kind character varying(16) NOT NULL,
    rater_id uuid NOT NULL,
    ratee_kind character varying(16) NOT NULL,
    ratee_id uuid NOT NULL,
    stars double precision NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT trip_ratings_ratee_kind_check CHECK (((ratee_kind)::text = ANY ((ARRAY['driver'::character varying, 'dispatcher'::character varying])::text[]))),
    CONSTRAINT trip_ratings_rater_kind_check CHECK (((rater_kind)::text = ANY ((ARRAY['driver'::character varying, 'dispatcher'::character varying, 'driver_manager'::character varying])::text[]))),
    CONSTRAINT trip_ratings_stars_range CHECK (((stars >= (1)::double precision) AND (stars <= (5)::double precision)))
);


--
-- Name: trip_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trip_stats (
    day_utc date NOT NULL,
    role character varying(32) NOT NULL,
    user_id uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    started_count bigint DEFAULT 0 NOT NULL,
    completed_count bigint DEFAULT 0 NOT NULL,
    cancelled_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: trip_user_notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trip_user_notifications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    trip_id uuid,
    recipient_kind character varying(16) NOT NULL,
    recipient_id uuid NOT NULL,
    event_kind character varying(64) NOT NULL,
    from_status character varying(32),
    to_status character varying(32),
    read_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    event_type character varying(32),
    payload jsonb,
    CONSTRAINT trip_user_notifications_recipient_kind_check CHECK (((recipient_kind)::text = ANY ((ARRAY['driver'::character varying, 'dispatcher'::character varying])::text[])))
);


--
-- Name: trips; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trips (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    offer_id uuid NOT NULL,
    driver_id uuid,
    status character varying(50) DEFAULT 'pending_driver'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    pending_confirm_to character varying(50),
    driver_confirmed_at timestamp with time zone,
    dispatcher_confirmed_at timestamp with time zone,
    agreed_price numeric(18,2) DEFAULT 0 NOT NULL,
    agreed_currency character varying(3) DEFAULT 'UZS'::character varying NOT NULL,
    rating_from_driver numeric(3,1),
    rating_from_dispatcher numeric(3,1),
    rating_driver_to_dm integer,
    rating_dm_to_driver integer,
    rating_dm_to_cm integer,
    rating_cm_to_dm integer,
    CONSTRAINT trips_pending_confirm_check CHECK (((pending_confirm_to IS NULL) OR ((pending_confirm_to)::text = ANY ((ARRAY['IN_TRANSIT'::character varying, 'DELIVERED'::character varying, 'COMPLETED'::character varying])::text[])))),
    CONSTRAINT trips_rating_cm_to_dm_check CHECK (((rating_cm_to_dm >= 1) AND (rating_cm_to_dm <= 5))),
    CONSTRAINT trips_rating_dm_to_cm_check CHECK (((rating_dm_to_cm >= 1) AND (rating_dm_to_cm <= 5))),
    CONSTRAINT trips_rating_dm_to_driver_check CHECK (((rating_dm_to_driver >= 1) AND (rating_dm_to_driver <= 5))),
    CONSTRAINT trips_rating_driver_to_dm_check CHECK (((rating_driver_to_dm >= 1) AND (rating_driver_to_dm <= 5))),
    CONSTRAINT trips_status_check CHECK (((status)::text = ANY ((ARRAY['IN_PROGRESS'::character varying, 'IN_TRANSIT'::character varying, 'DELIVERED'::character varying, 'COMPLETED'::character varying, 'CANCELLED'::character varying])::text[])))
);


--
-- Name: user_company_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_company_roles (
    user_id uuid NOT NULL,
    company_id uuid NOT NULL,
    role_id uuid NOT NULL,
    assigned_by uuid,
    assigned_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: user_login_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_login_stats (
    user_id uuid NOT NULL,
    role character varying(32) NOT NULL,
    total_logins bigint DEFAULT 0 NOT NULL,
    successful_logins bigint DEFAULT 0 NOT NULL,
    failed_logins bigint DEFAULT 0 NOT NULL,
    last_login_at_utc timestamp with time zone,
    total_session_duration_seconds bigint DEFAULT 0 NOT NULL,
    completed_sessions_count bigint DEFAULT 0 NOT NULL,
    avg_session_duration_seconds double precision DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: goadmin_menu id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_menu ALTER COLUMN id SET DEFAULT nextval('public.goadmin_menu_id_seq'::regclass);


--
-- Name: goadmin_operation_log id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_operation_log ALTER COLUMN id SET DEFAULT nextval('public.goadmin_operation_log_id_seq'::regclass);


--
-- Name: goadmin_permissions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_permissions ALTER COLUMN id SET DEFAULT nextval('public.goadmin_permissions_id_seq'::regclass);


--
-- Name: goadmin_roles id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_roles ALTER COLUMN id SET DEFAULT nextval('public.goadmin_roles_id_seq'::regclass);


--
-- Name: goadmin_session id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_session ALTER COLUMN id SET DEFAULT nextval('public.goadmin_session_id_seq'::regclass);


--
-- Name: goadmin_site id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_site ALTER COLUMN id SET DEFAULT nextval('public.goadmin_site_id_seq'::regclass);


--
-- Name: goadmin_users id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_users ALTER COLUMN id SET DEFAULT nextval('public.goadmin_users_id_seq'::regclass);


--
-- Name: admins admins_login_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admins
    ADD CONSTRAINT admins_login_key UNIQUE (login);


--
-- Name: admins admins_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admins
    ADD CONSTRAINT admins_pkey PRIMARY KEY (id);


--
-- Name: analytics_events analytics_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.analytics_events
    ADD CONSTRAINT analytics_events_pkey PRIMARY KEY (event_id);


--
-- Name: app_roles app_roles_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_roles
    ADD CONSTRAINT app_roles_name_key UNIQUE (name);


--
-- Name: app_roles app_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_roles
    ADD CONSTRAINT app_roles_pkey PRIMARY KEY (id);


--
-- Name: company_users app_users_phone_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.company_users
    ADD CONSTRAINT app_users_phone_key UNIQUE (phone);


--
-- Name: company_users app_users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.company_users
    ADD CONSTRAINT app_users_pkey PRIMARY KEY (id);


--
-- Name: archived_cargo archived_cargo_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.archived_cargo
    ADD CONSTRAINT archived_cargo_pkey PRIMARY KEY (id);


--
-- Name: archived_trips archived_trips_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.archived_trips
    ADD CONSTRAINT archived_trips_pkey PRIMARY KEY (id);


--
-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id);


--
-- Name: call_events call_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.call_events
    ADD CONSTRAINT call_events_pkey PRIMARY KEY (id);


--
-- Name: calls calls_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calls
    ADD CONSTRAINT calls_pkey PRIMARY KEY (id);


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_cargo_id_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_cargo_id_driver_id_key UNIQUE (cargo_id, driver_id);


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_pkey PRIMARY KEY (id);


--
-- Name: cargo_drivers cargo_drivers_cargo_id_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_drivers
    ADD CONSTRAINT cargo_drivers_cargo_id_driver_id_key UNIQUE (cargo_id, driver_id);


--
-- Name: cargo_drivers cargo_drivers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_drivers
    ADD CONSTRAINT cargo_drivers_pkey PRIMARY KEY (id);


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_pkey PRIMARY KEY (id);


--
-- Name: cargo_pending_photos cargo_pending_photos_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_pending_photos
    ADD CONSTRAINT cargo_pending_photos_pkey PRIMARY KEY (id);


--
-- Name: cargo_photos cargo_photos_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_photos
    ADD CONSTRAINT cargo_photos_pkey PRIMARY KEY (id);


--
-- Name: cargo cargo_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo
    ADD CONSTRAINT cargo_pkey PRIMARY KEY (id);


--
-- Name: cargo_stats cargo_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_stats
    ADD CONSTRAINT cargo_stats_pkey PRIMARY KEY (day_utc, role, user_id);


--
-- Name: cargo_types cargo_types_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_types
    ADD CONSTRAINT cargo_types_code_key UNIQUE (code);


--
-- Name: cargo_types cargo_types_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_types
    ADD CONSTRAINT cargo_types_pkey PRIMARY KEY (id);


--
-- Name: chat_attachments chat_attachments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_pkey PRIMARY KEY (id);


--
-- Name: chat_conversations chat_conv_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_conversations
    ADD CONSTRAINT chat_conv_unique UNIQUE (user_a_id, user_b_id);


--
-- Name: chat_conversation_reads chat_conversation_reads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_conversation_reads
    ADD CONSTRAINT chat_conversation_reads_pkey PRIMARY KEY (conversation_id, user_id);


--
-- Name: chat_conversations chat_conversations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_conversations
    ADD CONSTRAINT chat_conversations_pkey PRIMARY KEY (id);


--
-- Name: chat_messages chat_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_messages
    ADD CONSTRAINT chat_messages_pkey PRIMARY KEY (id);


--
-- Name: chat_source_hashes chat_source_hashes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_source_hashes
    ADD CONSTRAINT chat_source_hashes_pkey PRIMARY KEY (source_hash);


--
-- Name: cities cities_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cities
    ADD CONSTRAINT cities_code_key UNIQUE (code);


--
-- Name: cities cities_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cities
    ADD CONSTRAINT cities_pkey PRIMARY KEY (id);


--
-- Name: companies companies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.companies
    ADD CONSTRAINT companies_pkey PRIMARY KEY (id);


--
-- Name: deleted_freelance_dispatchers deleted_freelance_dispatchers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deleted_freelance_dispatchers
    ADD CONSTRAINT deleted_freelance_dispatchers_pkey PRIMARY KEY (id);


--
-- Name: dispatcher_company_roles dispatcher_company_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_company_roles
    ADD CONSTRAINT dispatcher_company_roles_pkey PRIMARY KEY (dispatcher_id, company_id);


--
-- Name: dispatcher_invitations dispatcher_invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_invitations
    ADD CONSTRAINT dispatcher_invitations_pkey PRIMARY KEY (id);


--
-- Name: dispatcher_invitations dispatcher_invitations_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_invitations
    ADD CONSTRAINT dispatcher_invitations_token_key UNIQUE (token);


--
-- Name: driver_cargo_favorites driver_cargo_favorites_driver_id_cargo_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_cargo_favorites
    ADD CONSTRAINT driver_cargo_favorites_driver_id_cargo_id_key UNIQUE (driver_id, cargo_id);


--
-- Name: driver_cargo_favorites driver_cargo_favorites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_cargo_favorites
    ADD CONSTRAINT driver_cargo_favorites_pkey PRIMARY KEY (id);


--
-- Name: driver_dispatcher_favorites driver_dispatcher_favorites_driver_id_dispatcher_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_dispatcher_favorites
    ADD CONSTRAINT driver_dispatcher_favorites_driver_id_dispatcher_id_key UNIQUE (driver_id, dispatcher_id);


--
-- Name: driver_dispatcher_favorites driver_dispatcher_favorites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_dispatcher_favorites
    ADD CONSTRAINT driver_dispatcher_favorites_pkey PRIMARY KEY (id);


--
-- Name: driver_invitations driver_invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_invitations
    ADD CONSTRAINT driver_invitations_pkey PRIMARY KEY (id);


--
-- Name: driver_invitations driver_invitations_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_invitations
    ADD CONSTRAINT driver_invitations_token_key UNIQUE (token);


--
-- Name: driver_manager_relations driver_manager_relations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_manager_relations
    ADD CONSTRAINT driver_manager_relations_pkey PRIMARY KEY (driver_id, manager_id);


--
-- Name: driver_powers driver_powers_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_powers
    ADD CONSTRAINT driver_powers_driver_id_key UNIQUE (driver_id);


--
-- Name: driver_powers driver_powers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_powers
    ADD CONSTRAINT driver_powers_pkey PRIMARY KEY (id);


--
-- Name: driver_to_dispatcher_invitations driver_to_dispatcher_invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_to_dispatcher_invitations
    ADD CONSTRAINT driver_to_dispatcher_invitations_pkey PRIMARY KEY (id);


--
-- Name: driver_to_dispatcher_invitations driver_to_dispatcher_invitations_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_to_dispatcher_invitations
    ADD CONSTRAINT driver_to_dispatcher_invitations_token_key UNIQUE (token);


--
-- Name: driver_trailers driver_trailers_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_trailers
    ADD CONSTRAINT driver_trailers_driver_id_key UNIQUE (driver_id);


--
-- Name: driver_trailers driver_trailers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_trailers
    ADD CONSTRAINT driver_trailers_pkey PRIMARY KEY (id);


--
-- Name: drivers drivers_phone_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.drivers
    ADD CONSTRAINT drivers_phone_key UNIQUE (phone);


--
-- Name: drivers drivers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.drivers
    ADD CONSTRAINT drivers_pkey PRIMARY KEY (id);


--
-- Name: freelance_dispatcher_cargo_favorites freelance_dispatcher_cargo_favorites_dispatcher_id_cargo_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_cargo_favorites
    ADD CONSTRAINT freelance_dispatcher_cargo_favorites_dispatcher_id_cargo_id_key UNIQUE (dispatcher_id, cargo_id);


--
-- Name: freelance_dispatcher_cargo_favorites freelance_dispatcher_cargo_favorites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_cargo_favorites
    ADD CONSTRAINT freelance_dispatcher_cargo_favorites_pkey PRIMARY KEY (id);


--
-- Name: freelance_dispatcher_driver_favorites freelance_dispatcher_driver_favorit_dispatcher_id_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_driver_favorites
    ADD CONSTRAINT freelance_dispatcher_driver_favorit_dispatcher_id_driver_id_key UNIQUE (dispatcher_id, driver_id);


--
-- Name: freelance_dispatcher_driver_favorites freelance_dispatcher_driver_favorites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_driver_favorites
    ADD CONSTRAINT freelance_dispatcher_driver_favorites_pkey PRIMARY KEY (id);


--
-- Name: freelance_dispatchers freelance_dispatchers_phone_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatchers
    ADD CONSTRAINT freelance_dispatchers_phone_key UNIQUE (phone);


--
-- Name: freelance_dispatchers freelance_dispatchers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatchers
    ADD CONSTRAINT freelance_dispatchers_pkey PRIMARY KEY (id);


--
-- Name: geo_stats geo_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.geo_stats
    ADD CONSTRAINT geo_stats_pkey PRIMARY KEY (day_utc, geo_city, role, user_id);


--
-- Name: goadmin_menu goadmin_menu_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_menu
    ADD CONSTRAINT goadmin_menu_pkey PRIMARY KEY (id);


--
-- Name: goadmin_operation_log goadmin_operation_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_operation_log
    ADD CONSTRAINT goadmin_operation_log_pkey PRIMARY KEY (id);


--
-- Name: goadmin_permissions goadmin_permissions_name_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_permissions
    ADD CONSTRAINT goadmin_permissions_name_unique UNIQUE (name);


--
-- Name: goadmin_permissions goadmin_permissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_permissions
    ADD CONSTRAINT goadmin_permissions_pkey PRIMARY KEY (id);


--
-- Name: goadmin_role_menu goadmin_role_menu_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_role_menu
    ADD CONSTRAINT goadmin_role_menu_unique UNIQUE (role_id, menu_id);


--
-- Name: goadmin_role_permissions goadmin_role_permissions_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_role_permissions
    ADD CONSTRAINT goadmin_role_permissions_unique UNIQUE (role_id, permission_id);


--
-- Name: goadmin_role_users goadmin_role_users_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_role_users
    ADD CONSTRAINT goadmin_role_users_unique UNIQUE (role_id, user_id);


--
-- Name: goadmin_roles goadmin_roles_name_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_roles
    ADD CONSTRAINT goadmin_roles_name_unique UNIQUE (name);


--
-- Name: goadmin_roles goadmin_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_roles
    ADD CONSTRAINT goadmin_roles_pkey PRIMARY KEY (id);


--
-- Name: goadmin_session goadmin_session_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_session
    ADD CONSTRAINT goadmin_session_pkey PRIMARY KEY (id);


--
-- Name: goadmin_site goadmin_site_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_site
    ADD CONSTRAINT goadmin_site_pkey PRIMARY KEY (id);


--
-- Name: goadmin_user_permissions goadmin_user_permissions_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_user_permissions
    ADD CONSTRAINT goadmin_user_permissions_unique UNIQUE (user_id, permission_id);


--
-- Name: goadmin_users goadmin_users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_users
    ADD CONSTRAINT goadmin_users_pkey PRIMARY KEY (id);


--
-- Name: goadmin_users goadmin_users_username_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_users
    ADD CONSTRAINT goadmin_users_username_unique UNIQUE (username);


--
-- Name: invitations invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_pkey PRIMARY KEY (id);


--
-- Name: invitations invitations_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_token_key UNIQUE (token);


--
-- Name: media_files media_files_content_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_content_hash_key UNIQUE (content_hash);


--
-- Name: media_files media_files_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_pkey PRIMARY KEY (id);


--
-- Name: offer_stats offer_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offer_stats
    ADD CONSTRAINT offer_stats_pkey PRIMARY KEY (day_utc, role, user_id);


--
-- Name: offers offers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offers
    ADD CONSTRAINT offers_pkey PRIMARY KEY (id);


--
-- Name: payments payments_cargo_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_cargo_id_key UNIQUE (cargo_id);


--
-- Name: payments payments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_pkey PRIMARY KEY (id);


--
-- Name: regions regions_country_code_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.regions
    ADD CONSTRAINT regions_country_code_code_key UNIQUE (country_code, code);


--
-- Name: regions regions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.regions
    ADD CONSTRAINT regions_pkey PRIMARY KEY (id);


--
-- Name: route_points route_points_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_points
    ADD CONSTRAINT route_points_pkey PRIMARY KEY (id);


--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (session_id);


--
-- Name: trip_ratings trip_ratings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_ratings
    ADD CONSTRAINT trip_ratings_pkey PRIMARY KEY (id);


--
-- Name: trip_ratings trip_ratings_trip_rater_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_ratings
    ADD CONSTRAINT trip_ratings_trip_rater_unique UNIQUE (trip_id, rater_kind, ratee_kind, ratee_id);


--
-- Name: trip_stats trip_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_stats
    ADD CONSTRAINT trip_stats_pkey PRIMARY KEY (day_utc, role, user_id);


--
-- Name: trip_user_notifications trip_user_notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_user_notifications
    ADD CONSTRAINT trip_user_notifications_pkey PRIMARY KEY (id);


--
-- Name: trips trips_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trips
    ADD CONSTRAINT trips_pkey PRIMARY KEY (id);


--
-- Name: user_company_roles user_company_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_pkey PRIMARY KEY (user_id, company_id, role_id);


--
-- Name: user_login_stats user_login_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_login_stats
    ADD CONSTRAINT user_login_stats_pkey PRIMARY KEY (user_id, role);


--
-- Name: goadmin_operation_log_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goadmin_operation_log_user_id_idx ON public.goadmin_operation_log USING btree (user_id);


--
-- Name: goadmin_role_menu_role_id_menu_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goadmin_role_menu_role_id_menu_id_idx ON public.goadmin_role_menu USING btree (role_id, menu_id);


--
-- Name: idx_admins_login; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admins_login ON public.admins USING btree (login);


--
-- Name: idx_analytics_events_entity_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_entity_time ON public.analytics_events USING btree (entity_type, entity_id, event_time_utc DESC);


--
-- Name: idx_analytics_events_geo_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_geo_time ON public.analytics_events USING btree (geo_city, event_time_utc DESC);


--
-- Name: idx_analytics_events_name_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_name_time ON public.analytics_events USING btree (event_name, event_time_utc DESC);


--
-- Name: idx_analytics_events_role_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_role_time ON public.analytics_events USING btree (role, event_time_utc DESC);


--
-- Name: idx_analytics_events_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_time ON public.analytics_events USING btree (event_time_utc DESC);


--
-- Name: idx_analytics_events_user_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_user_time ON public.analytics_events USING btree (user_id, event_time_utc DESC);


--
-- Name: idx_app_users_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_app_users_phone ON public.company_users USING btree (phone) WHERE (phone IS NOT NULL);


--
-- Name: idx_archived_cargo_archived_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_archived_cargo_archived_at ON public.archived_cargo USING btree (archived_at DESC);


--
-- Name: idx_archived_trips_archived_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_archived_trips_archived_at ON public.archived_trips USING btree (archived_at DESC);


--
-- Name: idx_archived_trips_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_archived_trips_cargo_id ON public.archived_trips USING btree (cargo_id);


--
-- Name: idx_audit_log_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_log_company ON public.audit_log USING btree (company_id);


--
-- Name: idx_audit_log_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_log_created ON public.audit_log USING btree (created_at DESC);


--
-- Name: idx_call_events_call_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_call_events_call_created ON public.call_events USING btree (call_id, created_at DESC);


--
-- Name: idx_calls_callee_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_calls_callee_created ON public.calls USING btree (callee_id, created_at DESC);


--
-- Name: idx_calls_caller_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_calls_caller_created ON public.calls USING btree (caller_id, created_at DESC);


--
-- Name: idx_calls_client_request_id_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_calls_client_request_id_uq ON public.calls USING btree (caller_id, client_request_id);


--
-- Name: idx_calls_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_calls_status_created ON public.calls USING btree (status, created_at DESC);


--
-- Name: idx_cargo_company_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_company_id ON public.cargo USING btree (company_id) WHERE (company_id IS NOT NULL);


--
-- Name: idx_cargo_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_created_at ON public.cargo USING btree (created_at);


--
-- Name: idx_cargo_created_by_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_created_by_id ON public.cargo USING btree (created_by_id) WHERE (created_by_id IS NOT NULL);


--
-- Name: idx_cargo_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_deleted_at ON public.cargo USING btree (deleted_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_cargo_driver_recommendations_cargo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_driver_recommendations_cargo ON public.cargo_driver_recommendations USING btree (cargo_id);


--
-- Name: idx_cargo_driver_recommendations_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_driver_recommendations_dispatcher ON public.cargo_driver_recommendations USING btree (invited_by_dispatcher_id);


--
-- Name: idx_cargo_driver_recommendations_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_driver_recommendations_driver ON public.cargo_driver_recommendations USING btree (driver_id);


--
-- Name: idx_cargo_drivers_cargo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_drivers_cargo ON public.cargo_drivers USING btree (cargo_id);


--
-- Name: idx_cargo_drivers_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_drivers_driver ON public.cargo_drivers USING btree (driver_id);


--
-- Name: idx_cargo_pending_photos_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_pending_photos_created ON public.cargo_pending_photos USING btree (created_at DESC);


--
-- Name: idx_cargo_photos_cargo_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_photos_cargo_created ON public.cargo_photos USING btree (cargo_id, created_at DESC);


--
-- Name: idx_cargo_power_plate_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_power_plate_type ON public.cargo USING btree (power_plate_type);


--
-- Name: idx_cargo_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_status ON public.cargo USING btree (status);


--
-- Name: idx_cargo_trailer_plate_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_trailer_plate_type ON public.cargo USING btree (trailer_plate_type);


--
-- Name: idx_cargo_truck_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_truck_type ON public.cargo USING btree (truck_type);


--
-- Name: idx_cargo_vehicles_amount; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_vehicles_amount ON public.cargo USING btree (vehicles_amount);


--
-- Name: idx_cargo_vehicles_left; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_vehicles_left ON public.cargo USING btree (vehicles_left);


--
-- Name: idx_cargo_weight; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_weight ON public.cargo USING btree (weight);


--
-- Name: idx_chat_attachments_conv_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_attachments_conv_created ON public.chat_attachments USING btree (conversation_id, created_at DESC);


--
-- Name: idx_chat_attachments_media_file; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_attachments_media_file ON public.chat_attachments USING btree (media_file_id);


--
-- Name: idx_chat_attachments_message_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_attachments_message_id ON public.chat_attachments USING btree (message_id);


--
-- Name: idx_chat_conversation_reads_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_conversation_reads_user ON public.chat_conversation_reads USING btree (user_id);


--
-- Name: idx_chat_conversations_user_a; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_conversations_user_a ON public.chat_conversations USING btree (user_a_id);


--
-- Name: idx_chat_conversations_user_b; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_conversations_user_b ON public.chat_conversations USING btree (user_b_id);


--
-- Name: idx_chat_messages_conversation_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_messages_conversation_created ON public.chat_messages USING btree (conversation_id, created_at DESC);


--
-- Name: idx_chat_messages_deleted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_messages_deleted ON public.chat_messages USING btree (deleted_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_chat_messages_sender; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_messages_sender ON public.chat_messages USING btree (sender_id);


--
-- Name: idx_chat_messages_undelivered; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_messages_undelivered ON public.chat_messages USING btree (conversation_id) WHERE ((delivered_at IS NULL) AND (deleted_at IS NULL));


--
-- Name: idx_chat_source_hashes_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_source_hashes_media ON public.chat_source_hashes USING btree (media_file_id);


--
-- Name: idx_chat_source_hashes_thumb; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_source_hashes_thumb ON public.chat_source_hashes USING btree (thumb_media_file_id);


--
-- Name: idx_cities_country; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cities_country ON public.cities USING btree (country_code);


--
-- Name: idx_cm_dm_offers_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cm_dm_offers_cargo_id ON public.cargo_manager_dm_offers USING btree (cargo_id, status, created_at DESC);


--
-- Name: idx_cm_dm_offers_cm_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cm_dm_offers_cm_id ON public.cargo_manager_dm_offers USING btree (cargo_manager_id, status, created_at DESC);


--
-- Name: idx_cm_dm_offers_dm_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cm_dm_offers_dm_id ON public.cargo_manager_dm_offers USING btree (driver_manager_id, status, created_at DESC);


--
-- Name: idx_companies_created_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_companies_created_by ON public.companies USING btree (created_by);


--
-- Name: idx_companies_inn_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_companies_inn_unique ON public.companies USING btree (inn) WHERE ((inn IS NOT NULL) AND ((inn)::text <> ''::text));


--
-- Name: idx_companies_license_number_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_companies_license_number_unique ON public.companies USING btree (license_number) WHERE ((license_number IS NOT NULL) AND ((license_number)::text <> ''::text));


--
-- Name: idx_companies_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_companies_name ON public.companies USING btree (name);


--
-- Name: idx_companies_owner_dispatcher_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_companies_owner_dispatcher_id ON public.companies USING btree (owner_dispatcher_id) WHERE (owner_dispatcher_id IS NOT NULL);


--
-- Name: idx_companies_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_companies_status ON public.companies USING btree (status);


--
-- Name: idx_company_users_company_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_company_users_company_id ON public.company_users USING btree (company_id) WHERE (company_id IS NOT NULL);


--
-- Name: idx_company_users_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_company_users_phone ON public.company_users USING btree (phone) WHERE (phone IS NOT NULL);


--
-- Name: idx_d2d_invitations_dispatcher_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_d2d_invitations_dispatcher_phone ON public.driver_to_dispatcher_invitations USING btree (replace(replace(replace(TRIM(BOTH FROM dispatcher_phone), ' '::text, ''::text), '-'::text, ''::text), '+'::text, ''::text));


--
-- Name: idx_d2d_invitations_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_d2d_invitations_driver ON public.driver_to_dispatcher_invitations USING btree (driver_id);


--
-- Name: idx_d2d_invitations_driver_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_d2d_invitations_driver_status_created ON public.driver_to_dispatcher_invitations USING btree (driver_id, status, created_at DESC);


--
-- Name: idx_d2d_invitations_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_d2d_invitations_token ON public.driver_to_dispatcher_invitations USING btree (token);


--
-- Name: idx_deleted_drivers_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_drivers_phone ON public.deleted_drivers USING btree (phone);


--
-- Name: idx_deleted_freelance_dispatchers_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_freelance_dispatchers_cargo_id ON public.deleted_freelance_dispatchers USING btree (cargo_id);


--
-- Name: idx_deleted_freelance_dispatchers_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_freelance_dispatchers_driver_id ON public.deleted_freelance_dispatchers USING btree (driver_id);


--
-- Name: idx_deleted_freelance_dispatchers_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_freelance_dispatchers_phone ON public.deleted_freelance_dispatchers USING btree (phone);


--
-- Name: idx_deleted_freelance_dispatchers_pinfl; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_freelance_dispatchers_pinfl ON public.deleted_freelance_dispatchers USING btree (pinfl);


--
-- Name: idx_dispatcher_company_roles_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dispatcher_company_roles_company ON public.dispatcher_company_roles USING btree (company_id);


--
-- Name: idx_dispatcher_company_roles_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dispatcher_company_roles_dispatcher ON public.dispatcher_company_roles USING btree (dispatcher_id);


--
-- Name: idx_dispatcher_invitations_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dispatcher_invitations_company ON public.dispatcher_invitations USING btree (company_id);


--
-- Name: idx_dispatcher_invitations_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dispatcher_invitations_phone ON public.dispatcher_invitations USING btree (phone);


--
-- Name: idx_dispatcher_invitations_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dispatcher_invitations_token ON public.dispatcher_invitations USING btree (token);


--
-- Name: idx_driver_cargo_favorites_cargo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_cargo_favorites_cargo ON public.driver_cargo_favorites USING btree (cargo_id);


--
-- Name: idx_driver_cargo_favorites_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_cargo_favorites_driver ON public.driver_cargo_favorites USING btree (driver_id);


--
-- Name: idx_driver_dispatcher_favorites_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_dispatcher_favorites_dispatcher ON public.driver_dispatcher_favorites USING btree (dispatcher_id);


--
-- Name: idx_driver_dispatcher_favorites_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_dispatcher_favorites_driver ON public.driver_dispatcher_favorites USING btree (driver_id);


--
-- Name: idx_driver_invitations_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_company ON public.driver_invitations USING btree (company_id);


--
-- Name: idx_driver_invitations_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_dispatcher ON public.driver_invitations USING btree (invited_by_dispatcher_id) WHERE (invited_by_dispatcher_id IS NOT NULL);


--
-- Name: idx_driver_invitations_invited_by_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_invited_by_status_created ON public.driver_invitations USING btree (invited_by, status, created_at DESC);


--
-- Name: idx_driver_invitations_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_phone ON public.driver_invitations USING btree (phone);


--
-- Name: idx_driver_invitations_phone_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_phone_status_created ON public.driver_invitations USING btree (replace(replace(replace(TRIM(BOTH FROM phone), ' '::text, ''::text), '-'::text, ''::text), '+'::text, ''::text), status, created_at DESC);


--
-- Name: idx_driver_invitations_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_driver_invitations_token ON public.driver_invitations USING btree (token);


--
-- Name: idx_driver_manager_relations_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_manager_relations_driver ON public.driver_manager_relations USING btree (driver_id);


--
-- Name: idx_driver_manager_relations_manager; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_manager_relations_manager ON public.driver_manager_relations USING btree (manager_id);


--
-- Name: idx_driver_powers_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_powers_driver_id ON public.driver_powers USING btree (driver_id);


--
-- Name: idx_driver_trailers_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_trailers_driver_id ON public.driver_trailers USING btree (driver_id);


--
-- Name: idx_drivers_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_drivers_phone ON public.drivers USING btree (phone);


--
-- Name: idx_freelance_dispatcher_cargo_favorites_cargo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatcher_cargo_favorites_cargo ON public.freelance_dispatcher_cargo_favorites USING btree (cargo_id);


--
-- Name: idx_freelance_dispatcher_cargo_favorites_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatcher_cargo_favorites_dispatcher ON public.freelance_dispatcher_cargo_favorites USING btree (dispatcher_id);


--
-- Name: idx_freelance_dispatcher_driver_favorites_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatcher_driver_favorites_dispatcher ON public.freelance_dispatcher_driver_favorites USING btree (dispatcher_id);


--
-- Name: idx_freelance_dispatcher_driver_favorites_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatcher_driver_favorites_driver ON public.freelance_dispatcher_driver_favorites USING btree (driver_id);


--
-- Name: idx_freelance_dispatchers_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatchers_cargo_id ON public.freelance_dispatchers USING btree (cargo_id);


--
-- Name: idx_freelance_dispatchers_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatchers_driver_id ON public.freelance_dispatchers USING btree (driver_id);


--
-- Name: idx_freelance_dispatchers_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatchers_phone ON public.freelance_dispatchers USING btree (phone);


--
-- Name: idx_freelance_dispatchers_pinfl; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatchers_pinfl ON public.freelance_dispatchers USING btree (pinfl);


--
-- Name: idx_geo_stats_day_city; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_geo_stats_day_city ON public.geo_stats USING btree (day_utc DESC, geo_city);


--
-- Name: idx_invitations_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invitations_company ON public.invitations USING btree (company_id);


--
-- Name: idx_invitations_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invitations_email ON public.invitations USING btree (email);


--
-- Name: idx_invitations_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_invitations_token ON public.invitations USING btree (token);


--
-- Name: idx_media_files_kind_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_files_kind_created ON public.media_files USING btree (kind, created_at DESC);


--
-- Name: idx_offers_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_offers_cargo_id ON public.offers USING btree (cargo_id);


--
-- Name: idx_offers_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_offers_status ON public.offers USING btree (status);


--
-- Name: idx_payments_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_payments_cargo_id ON public.payments USING btree (cargo_id);


--
-- Name: idx_regions_country; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_regions_country ON public.regions USING btree (country_code);


--
-- Name: idx_route_points_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_points_cargo_id ON public.route_points USING btree (cargo_id);


--
-- Name: idx_route_points_country_code; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_points_country_code ON public.route_points USING btree (country_code);


--
-- Name: idx_sessions_geo_started; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_geo_started ON public.sessions USING btree (geo_city, started_at_utc DESC);


--
-- Name: idx_sessions_role_started; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_role_started ON public.sessions USING btree (role, started_at_utc DESC);


--
-- Name: idx_sessions_user_started; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_user_started ON public.sessions USING btree (user_id, started_at_utc DESC);


--
-- Name: idx_trip_ratings_ratee; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trip_ratings_ratee ON public.trip_ratings USING btree (ratee_kind, ratee_id);


--
-- Name: idx_trips_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trips_cargo_id ON public.trips USING btree (cargo_id);


--
-- Name: idx_trips_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trips_driver_id ON public.trips USING btree (driver_id) WHERE (driver_id IS NOT NULL);


--
-- Name: idx_trips_offer_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trips_offer_id ON public.trips USING btree (offer_id);


--
-- Name: idx_trips_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trips_status ON public.trips USING btree (status);


--
-- Name: idx_tun_recipient_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tun_recipient_created ON public.trip_user_notifications USING btree (recipient_kind, recipient_id, created_at DESC);


--
-- Name: idx_tun_recipient_type_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tun_recipient_type_created ON public.trip_user_notifications USING btree (recipient_kind, recipient_id, event_type, created_at DESC);


--
-- Name: idx_tun_trip; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tun_trip ON public.trip_user_notifications USING btree (trip_id);


--
-- Name: idx_tun_unread; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tun_unread ON public.trip_user_notifications USING btree (recipient_kind, recipient_id) WHERE (read_at IS NULL);


--
-- Name: idx_user_company_roles_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_company_roles_company ON public.user_company_roles USING btree (company_id);


--
-- Name: idx_user_company_roles_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_company_roles_user ON public.user_company_roles USING btree (user_id);


--
-- Name: ux_cargo_drivers_driver_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ux_cargo_drivers_driver_active ON public.cargo_drivers USING btree (driver_id) WHERE ((status)::text = 'ACTIVE'::text);


--
-- Name: admins admins_before_save_password; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admins_before_save_password BEFORE INSERT OR UPDATE OF password ON public.admins FOR EACH ROW EXECUTE FUNCTION public.admins_hash_password_trigger();


--
-- Name: audit_log audit_log_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id);


--
-- Name: audit_log audit_log_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.company_users(id);


--
-- Name: call_events call_events_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.call_events
    ADD CONSTRAINT call_events_call_id_fkey FOREIGN KEY (call_id) REFERENCES public.calls(id) ON DELETE CASCADE;


--
-- Name: calls calls_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calls
    ADD CONSTRAINT calls_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.chat_conversations(id) ON DELETE SET NULL;


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_invited_by_dispatcher_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_invited_by_dispatcher_id_fkey FOREIGN KEY (invited_by_dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: cargo_drivers cargo_drivers_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_drivers
    ADD CONSTRAINT cargo_drivers_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: cargo_drivers cargo_drivers_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_drivers
    ADD CONSTRAINT cargo_drivers_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_cargo_manager_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_cargo_manager_id_fkey FOREIGN KEY (cargo_manager_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE SET NULL;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_driver_manager_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_driver_manager_id_fkey FOREIGN KEY (driver_manager_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_offer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_offer_id_fkey FOREIGN KEY (offer_id) REFERENCES public.offers(id) ON DELETE SET NULL;


--
-- Name: cargo_photos cargo_photos_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_photos
    ADD CONSTRAINT cargo_photos_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: chat_attachments chat_attachments_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.chat_conversations(id) ON DELETE CASCADE;


--
-- Name: chat_attachments chat_attachments_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE RESTRICT;


--
-- Name: chat_attachments chat_attachments_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.chat_messages(id) ON DELETE SET NULL;


--
-- Name: chat_attachments chat_attachments_thumb_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_thumb_media_file_id_fkey FOREIGN KEY (thumb_media_file_id) REFERENCES public.media_files(id) ON DELETE RESTRICT;


--
-- Name: chat_conversation_reads chat_conversation_reads_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_conversation_reads
    ADD CONSTRAINT chat_conversation_reads_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.chat_conversations(id) ON DELETE CASCADE;


--
-- Name: chat_messages chat_messages_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_messages
    ADD CONSTRAINT chat_messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.chat_conversations(id) ON DELETE CASCADE;


--
-- Name: chat_source_hashes chat_source_hashes_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_source_hashes
    ADD CONSTRAINT chat_source_hashes_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE CASCADE;


--
-- Name: chat_source_hashes chat_source_hashes_thumb_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_source_hashes
    ADD CONSTRAINT chat_source_hashes_thumb_media_file_id_fkey FOREIGN KEY (thumb_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: dispatcher_company_roles dispatcher_company_roles_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_company_roles
    ADD CONSTRAINT dispatcher_company_roles_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- Name: dispatcher_invitations dispatcher_invitations_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_invitations
    ADD CONSTRAINT dispatcher_invitations_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- Name: driver_cargo_favorites driver_cargo_favorites_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_cargo_favorites
    ADD CONSTRAINT driver_cargo_favorites_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: driver_cargo_favorites driver_cargo_favorites_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_cargo_favorites
    ADD CONSTRAINT driver_cargo_favorites_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: driver_dispatcher_favorites driver_dispatcher_favorites_dispatcher_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_dispatcher_favorites
    ADD CONSTRAINT driver_dispatcher_favorites_dispatcher_id_fkey FOREIGN KEY (dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: driver_dispatcher_favorites driver_dispatcher_favorites_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_dispatcher_favorites
    ADD CONSTRAINT driver_dispatcher_favorites_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: driver_invitations driver_invitations_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_invitations
    ADD CONSTRAINT driver_invitations_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- Name: driver_manager_relations driver_manager_relations_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_manager_relations
    ADD CONSTRAINT driver_manager_relations_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: driver_manager_relations driver_manager_relations_manager_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_manager_relations
    ADD CONSTRAINT driver_manager_relations_manager_id_fkey FOREIGN KEY (manager_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: driver_powers driver_powers_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_powers
    ADD CONSTRAINT driver_powers_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: driver_to_dispatcher_invitations driver_to_dispatcher_invitations_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_to_dispatcher_invitations
    ADD CONSTRAINT driver_to_dispatcher_invitations_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: driver_trailers driver_trailers_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_trailers
    ADD CONSTRAINT driver_trailers_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: cargo fk_cargo_cargo_type; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo
    ADD CONSTRAINT fk_cargo_cargo_type FOREIGN KEY (cargo_type_id) REFERENCES public.cargo_types(id);


--
-- Name: cargo fk_cargo_company_id; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo
    ADD CONSTRAINT fk_cargo_company_id FOREIGN KEY (company_id) REFERENCES public.companies(id);


--
-- Name: companies fk_companies_created_by_admins; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.companies
    ADD CONSTRAINT fk_companies_created_by_admins FOREIGN KEY (created_by) REFERENCES public.admins(id);


--
-- Name: companies fk_companies_owner_app_users; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.companies
    ADD CONSTRAINT fk_companies_owner_app_users FOREIGN KEY (owner_id) REFERENCES public.company_users(id);


--
-- Name: companies fk_companies_owner_dispatcher; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.companies
    ADD CONSTRAINT fk_companies_owner_dispatcher FOREIGN KEY (owner_dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE SET NULL;


--
-- Name: company_users fk_company_users_company; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.company_users
    ADD CONSTRAINT fk_company_users_company FOREIGN KEY (company_id) REFERENCES public.companies(id);


--
-- Name: dispatcher_company_roles fk_dcr_dispatcher; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_company_roles
    ADD CONSTRAINT fk_dcr_dispatcher FOREIGN KEY (dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: driver_invitations fk_driver_invitations_dispatcher; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_invitations
    ADD CONSTRAINT fk_driver_invitations_dispatcher FOREIGN KEY (invited_by_dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE SET NULL;


--
-- Name: trips fk_trips_driver; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trips
    ADD CONSTRAINT fk_trips_driver FOREIGN KEY (driver_id) REFERENCES public.drivers(id);


--
-- Name: trips fk_trips_offer; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trips
    ADD CONSTRAINT fk_trips_offer FOREIGN KEY (offer_id) REFERENCES public.offers(id);


--
-- Name: freelance_dispatcher_cargo_favorites freelance_dispatcher_cargo_favorites_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_cargo_favorites
    ADD CONSTRAINT freelance_dispatcher_cargo_favorites_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: freelance_dispatcher_cargo_favorites freelance_dispatcher_cargo_favorites_dispatcher_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_cargo_favorites
    ADD CONSTRAINT freelance_dispatcher_cargo_favorites_dispatcher_id_fkey FOREIGN KEY (dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: freelance_dispatcher_driver_favorites freelance_dispatcher_driver_favorites_dispatcher_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_driver_favorites
    ADD CONSTRAINT freelance_dispatcher_driver_favorites_dispatcher_id_fkey FOREIGN KEY (dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: freelance_dispatcher_driver_favorites freelance_dispatcher_driver_favorites_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_driver_favorites
    ADD CONSTRAINT freelance_dispatcher_driver_favorites_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: invitations invitations_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- Name: invitations invitations_invited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.company_users(id);


--
-- Name: invitations invitations_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.app_roles(id);


--
-- Name: offers offers_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offers
    ADD CONSTRAINT offers_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: payments payments_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: route_points route_points_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_points
    ADD CONSTRAINT route_points_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: trip_ratings trip_ratings_trip_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_ratings
    ADD CONSTRAINT trip_ratings_trip_id_fkey FOREIGN KEY (trip_id) REFERENCES public.trips(id) ON DELETE CASCADE;


--
-- Name: trips trips_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trips
    ADD CONSTRAINT trips_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: user_company_roles user_company_roles_assigned_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_assigned_by_fkey FOREIGN KEY (assigned_by) REFERENCES public.company_users(id);


--
-- Name: user_company_roles user_company_roles_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- Name: user_company_roles user_company_roles_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.app_roles(id);


--
-- Name: user_company_roles user_company_roles_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.company_users(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict XhfTMsZsakoI0kcp5V1zCnhkFDp6rw5PYcOJbIRnulj0iN5v9ZfcDGPY8GDHPa6

