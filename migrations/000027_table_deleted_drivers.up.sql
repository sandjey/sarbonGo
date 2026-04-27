--
-- PostgreSQL database dump
--


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

SET default_tablespace = '';

SET default_table_access_method = heap;

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
-- Name: idx_deleted_drivers_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_drivers_phone ON public.deleted_drivers USING btree (phone);


--
-- PostgreSQL database dump complete
--


